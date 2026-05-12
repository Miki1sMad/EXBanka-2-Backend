// bank-service entrypoint.
//
// bank-service entrypoint.
//
// Starts two servers concurrently:
//   - gRPC server          on 0.0.0.0:50051  (standard net/grpc)
//   - gRPC-Gateway HTTP    on 0.0.0.0:8080   (grpc-gateway/v2 runtime.ServeMux)
//
// The HTTP gateway is a reverse-proxy that translates REST calls into gRPC
// calls against the local gRPC server at localhost:50051.
//
// All configuration is loaded from environment variables via internal/config.
package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	pbactuary "banka-backend/proto/actuary"
	pb "banka-backend/proto/banka"
	"banka-backend/services/bank-service/internal/config"
	"banka-backend/services/bank-service/internal/domain"
	"banka-backend/services/bank-service/internal/handler"
	"banka-backend/services/bank-service/internal/repository"
	"banka-backend/services/bank-service/internal/service"
	"banka-backend/services/bank-service/internal/trading"
	tradingworker "banka-backend/services/bank-service/internal/trading/worker"
	"banka-backend/services/bank-service/internal/transport"
	"banka-backend/services/bank-service/internal/worker"
	auth "banka-backend/shared/auth"
	"banka-backend/shared/metrics"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// grpcLocalHost is the hostname the gRPC-Gateway uses to dial back to the
// local gRPC server. Port is extracted from cfg.GRPCAddr at runtime so the
// two always stay in sync regardless of the GRPC_ADDR env var.
const grpcLocalHost = "localhost"

func main() {
	// ── 1. Config ────────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[main] config error: %v", err)
	}

	// ── 2. Database (GORM + PostgreSQL) ──────────────────────────────────────
	db, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("[db] open: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("[db] get underlying sql.DB: %v", err)
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		log.Fatalf("[db] ping: %v", err)
	}
	log.Println("[db] connected to PostgreSQL")

	// ── 3. Wire-up slojeva ───────────────────────────────────────────────────
	currencyRepo := repository.NewCurrencyRepository(db)
	currencyService := service.NewCurrencyService(currencyRepo)

	delatnostRepo := repository.NewDelatnostRepository(db)
	delatnostService := service.NewDelatnostService(delatnostRepo)

	accountRepo := repository.NewAccountRepository(db)
	accountService := service.NewAccountService(accountRepo, currencyRepo)

	recipientRepo := repository.NewPaymentRecipientRepository(db)
	paymentRepo := repository.NewPaymentRepository(db)
	paymentService := service.NewPaymentService(recipientRepo, paymentRepo)

	kreditRepo := repository.NewKreditRepository(db)
	kreditService := service.NewKreditService(kreditRepo)

	karticaRepo := repository.NewKarticaRepository(db)
	berzaRepo := repository.NewBerzaRepository(db)

	// ── Redis store za OTP state (Flow 2) ────────────────────────────────────
	var redisStore domain.CardRequestStore
	var marketModeStore domain.MarketModeStore
	if cfg.RedisURL != "" {
		rs, err := transport.NewRedisCardRequestStore(cfg.RedisURL)
		if err != nil {
			log.Fatalf("[main] Redis konekcija: %v", err)
		}
		redisStore = rs

		ms, err := transport.NewRedisMarketModeStore(cfg.RedisURL)
		if err != nil {
			log.Fatalf("[main] Redis market mode store: %v", err)
		}
		marketModeStore = ms
	} else {
		log.Printf("[main] REDIS_URL nije postavljen — /api/cards/request neće biti funkcionalan")
		redisStore = &transport.NoOpCardRequestStore{}
		marketModeStore = &transport.NoOpMarketModeStore{}
	}

	// ── Notification-service gRPC klijent (sinhronizovano slanje OTP emaila) ─
	var notifClient domain.NotificationSender
	if cfg.NotificationServiceAddr != "" {
		nc, err := transport.NewNotificationServiceClient(cfg.NotificationServiceAddr)
		if err != nil {
			log.Fatalf("[main] notification-service gRPC klijent: %v", err)
		}
		defer nc.Close()
		notifClient = nc
		log.Printf("[main] notification-service gRPC klijent konfigurisan na %s", cfg.NotificationServiceAddr)
	} else {
		log.Printf("[main] NOTIFICATION_SERVICE_ADDR nije postavljen — OTP emailovi neće biti slani")
		notifClient = &transport.NoOpNotificationSender{}
	}

	karticaService := service.NewKarticaService(karticaRepo, cfg.CVVPepper, redisStore, notifClient)
	berzaService := service.NewBerzaService(berzaRepo, marketModeStore)

	listingRepo := repository.NewListingRepository(db)
	listingHTTP := &http.Client{Timeout: 28 * time.Second}
	listingService := service.NewListingService(listingRepo, listingHTTP, cfg.EODHDAPIKey)

	// ── InstallmentWorker (cron job za automatsku naplatu rata) ───────────────
	var notifPublisher worker.NotificationPublisher
	if cfg.RabbitMQURL != "" {
		log.Printf("[main] RabbitMQ konfigurisan — kreditne notifikacije će biti slane")
		notifPublisher = worker.NewAMQPKreditPublisher(cfg.RabbitMQURL)
	} else {
		log.Printf("[main] RABBITMQ_URL nije postavljen — kreditne notifikacije se samo loguju")
		notifPublisher = &worker.NoOpNotificationPublisher{}
	}

	installmentWorker := worker.NewInstallmentWorker(
		kreditRepo,
		notifPublisher,
		time.Duration(cfg.WorkerIntervalHours)*time.Hour,
		time.Duration(cfg.RetryAfterHours)*time.Hour,
		cfg.LatePaymentPenalty,
	)

	// ── User-service gRPC klijent (za validaciju klijenta pri kreiranju računa) ─
	userClient, err := transport.NewUserServiceClient(cfg.UserServiceAddr)
	if err != nil {
		log.Fatalf("[main] user-service gRPC client: %v", err)
	}
	defer userClient.Close()
	log.Printf("[main] user-service gRPC klijent konfigurisan na %s", cfg.UserServiceAddr)

	// ── Account email publisher ───────────────────────────────────────────────
	var accountPublisher worker.AccountEmailPublisher
	if cfg.RabbitMQURL != "" {
		accountPublisher = worker.NewAMQPAccountPublisher(cfg.RabbitMQURL)
	} else {
		accountPublisher = &worker.NoOpAccountEmailPublisher{}
	}

	actuaryRepo := repository.NewActuaryRepository(sqlDB)
	actuaryService := service.NewActuaryService(actuaryRepo)
	actuaryHandler := handler.NewActuaryHandler(actuaryService)

	exchangeProvider := repository.NewExchangeRateProvider(cfg.ExchangeRateAPIKey, cfg.ExchangeRateAPIBaseURL)
	exchangeTransferRepo := repository.NewExchangeTransferRepository(db)
	exchangeService := service.NewExchangeService(exchangeProvider, exchangeTransferRepo, cfg.ExchangeSpreadRate, cfg.ExchangeProvizijaRate)

	orderRepo := repository.NewOrderRepository(db)
	marginChecker := repository.NewMarginChecker(db, exchangeService)
	fundsManager := repository.NewFundsManager(db, exchangeService, cfg.ExchangeProvizijaRate)
	tradingService := trading.NewTradingService(orderRepo, listingService, actuaryRepo, marginChecker, fundsManager)

	// ── Trading engine (async order execution) ────────────────────────────────
	// tickBus povezuje ListingRefresherWorker (publisher) sa trading engine-om
	// (subscriber) za event-driven okidanje LIMIT naloga.
	tickBus := tradingworker.NewPriceTickBus()
	marketDataProvider := tradingworker.NewListingMarketDataProvider(listingRepo)

	// investmentFundRepo is created early so the trading engine can reference it.
	// The full service is wired below after other dependencies are ready.
	investmentFundRepo := repository.NewInvestmentFundRepository(db)

	tradingEngine := tradingworker.NewEngine(orderRepo, marketDataProvider, fundsManager, berzaService, db, 0, investmentFundRepo, tickBus)

	bankHandler := handler.NewBankHandler(currencyService, delatnostService, accountService, paymentService, kreditService, karticaService, berzaService, listingService, exchangeService, tradingService, userClient, accountPublisher)

	receiptHandler := handler.NewPaymentReceiptHandler(paymentService, cfg.JWTAccessSecret)
	marketModeHTTPHandler := handler.NewMarketModeHTTPHandler(marketModeStore, cfg.JWTAccessSecret)
	exchangeTransferHandler := handler.NewExchangeTransferHandler(paymentService, cfg.JWTAccessSecret)
	exchangeRateHandler := handler.NewExchangeRateHandler(exchangeService, cfg.JWTAccessSecret)
	karticaRequestHandler := handler.NewKarticaRequestHandler(karticaService, userClient, cfg.JWTAccessSecret, accountPublisher)
	klientKarticeHandler := handler.NewKlientKarticeHandler(karticaService, cfg.JWTAccessSecret)

	// TaxService — jedinstvena poslovna logika za obračun i naplatu poreza na
	// kapitalnu dobit (15%). Deli je TaxHandler, MonthlyTaxWorker i PortfolioHandler.
	taxService := service.NewTaxService(db, exchangeService, userClient, cfg.StateRevenueAccountID)

	portfolioHandler := handler.NewPortfolioHandler(db, listingService, taxService, cfg.JWTAccessSecret)
	taxHandler := handler.NewTaxHandler(taxService, cfg.JWTAccessSecret)
	myOrdersHandler := handler.NewMyOrdersHandler(tradingService, cfg.JWTAccessSecret)
	tradingFXHandler := handler.NewTradingFXHandler(tradingService, accountService, exchangeService, cfg.JWTAccessSecret)

	// ── InvestmentFundHandler — Discovery, Details, Create, FundOrder (Celina 4) ─
	investmentFundService := service.NewInvestmentFundService(
		investmentFundRepo,
		listingService,
		exchangeService,
		accountService,
		currencyRepo,
	)
	investmentFundHandler := handler.NewInvestmentFundHandler(
		investmentFundService,
		investmentFundRepo,
		tradingService,
		listingService,
		exchangeService,
		userClient,
		cfg.JWTAccessSecret,
	)

	fundHandler := handler.NewFundHandler(db, exchangeService, investmentFundService, cfg.JWTAccessSecret)

	// InternalActuaryHandler proširen za prenos menadžmenta fondova
	internalActuaryHandler := handler.NewInternalActuaryHandler(actuaryService, investmentFundService, cfg.JWTAccessSecret)

	// ── BankProfitHandler — actuary-performance + fund-positions (Celina 4) ──
	bankProfitHandler := handler.NewBankProfitHandler(db, investmentFundService, exchangeService, userClient, cfg.JWTAccessSecret)

	// ── OTC (Faza 2) ─────────────────────────────────────────────────────────
	// PaymentService igra ulogu OTCPaymentPort — premija ide kroz auditovanu
	// putanju (knjiženja u core_banking.transakcija + FX kroz trezor banke).
	otcRepo := repository.NewOTCRepository(db)
	otcService := service.NewOTCService(db, otcRepo, paymentService)
	otcHandler := handler.NewOTCHandler(otcService, cfg.JWTAccessSecret, cfg.OwnBankID, userClient)

	// ── OTC Contracts & SAGA (Celina 4) ──────────────────────────────────────
	// OTCContractService orkestira SAGA tok za izvršavanje ("Iskoristi") OTC ugovora.
	// OTCSagaRepository čuva stanje svake SAGA egzekucije u bazi za recovery.
	otcSagaRepo := repository.NewOTCSagaRepository(db)
	otcContractService := service.NewOTCContractService(
		db,
		otcRepo,
		otcSagaRepo,
		listingService,
		exchangeService,
	)
	otcContractHandler := handler.NewOTCContractHandler(otcContractService, userClient, cfg.JWTAccessSecret)

	// ── Interbank (si-tx-proto) ──────────────────────────────────────────────
	interbankRepo := repository.NewInterbankRepository(db)
	interbankClient := service.NewInterbankClient(service.InterbankClientConfig{
		PeerBaseURL:        cfg.InterbankPeerBaseURL,
		PeerAPIKey:         cfg.InterbankPeerAPIKey,
		PeerRoutingNumber:  cfg.InterbankPeerRoutingNumber,
		OurRoutingNumber:   cfg.InterbankRoutingNumber,
		HTTPTimeoutSeconds: cfg.InterbankHTTPTimeoutSeconds,
	})
	interbankMsgSvc := service.NewInterbankMessageService(
		interbankRepo,
		interbankClient,
		cfg.InterbankRoutingNumber,
		cfg.InterbankRetryMaxAttempts,
		cfg.InterbankRetryBackoffSeconds,
	)
	interbankExecutor := service.NewLocalTransactionExecutor(
		db,
		interbankRepo,
		cfg.InterbankRoutingNumber,
		"", // accountPrefix se izvodi iz routingNumber-a
	)
	interbankCoordinator := service.NewTransactionCoordinator(
		db,
		interbankRepo,
		interbankExecutor,
		interbankMsgSvc,
		interbankClient,
		cfg.InterbankRoutingNumber,
		cfg.InterbankPeerRoutingNumber,
		"",
	)
	interbankOTCSvc := service.NewInterbankOTCService(interbankRepo, cfg.InterbankRoutingNumber)
	interbankOptionSvc := service.NewInterbankOptionContractService(
		interbankRepo,
		interbankCoordinator,
		cfg.InterbankRoutingNumber,
	)
	interbankProtoHandler := handler.NewInterbankHandler(
		interbankCoordinator,
		interbankOTCSvc,
		interbankOptionSvc,
		interbankRepo,
		cfg.InterbankAPIKey,
		cfg.InterbankRoutingNumber,
		cfg.InterbankBankDisplayName,
		userClient,
	)
	interbankClientHandler := handler.NewInterbankPaymentHandler(
		interbankCoordinator,
		interbankOTCSvc,
		interbankOptionSvc,
		interbankClient,
		interbankRepo,
		cfg.JWTAccessSecret,
		cfg.InterbankRoutingNumber,
		userClient,
	)
	if cfg.InterbankPeerBaseURL == "" {
		log.Printf("[interbank] INTERBANK_PEER_BASE_URL nije postavljen — slanje međubankarskih poruka će vraćati grešku konfiguracije sve dok se ne podesi")
	} else {
		log.Printf("[interbank] peer URL=%s, peerRoutingNumber=%d, ourRoutingNumber=%d",
			cfg.InterbankPeerBaseURL, cfg.InterbankPeerRoutingNumber, cfg.InterbankRoutingNumber)
	}

	// ── 4. Auth interceptor ──────────────────────────────────────────────────
	// Sve rute zahtevaju validan JWT access token osim gRPC health check-a.
	authInterceptor := auth.NewAuthInterceptor(cfg.JWTAccessSecret, []string{
		"/grpc.health.v1.Health/Check",
	})

	// ── 5. gRPC server ───────────────────────────────────────────────────────
	// Prometheus gRPC server metrike — interceptor je PRVI u chainu (pre auth)
	// kako bi se merili i unauthorized pozivi.
	srvMetrics := metrics.NewServerMetrics()
	grpcSrv := transport.NewGRPCServer(cfg.GRPCAddr, srvMetrics.UnaryServerInterceptor(), authInterceptor.Unary())
	pb.RegisterBankaServiceServer(grpcSrv.Server(), bankHandler)
	pbactuary.RegisterActuaryServiceServer(grpcSrv.Server(), actuaryHandler)
	// Inicijalizuj nulte vrednosti za sve metode (lepše Grafana grafike pre prvog poziva).
	srvMetrics.InitializeMetrics(grpcSrv.Server())

	// ── 6. gRPC-Gateway: dial the local gRPC server ──────────────────────────
	// Derive the gateway target from cfg.GRPCAddr so they always stay in sync.
	// cfg.GRPCAddr is e.g. "0.0.0.0:50051"; we replace the bind host with
	// localhost because gateway and gRPC server share the same process.
	_, grpcPort, err := net.SplitHostPort(cfg.GRPCAddr)
	if err != nil {
		log.Fatalf("[gateway] invalid GRPC_ADDR %q: %v", cfg.GRPCAddr, err)
	}
	grpcLocalTarget := net.JoinHostPort(grpcLocalHost, grpcPort)
	log.Printf("[gateway] gRPC local target: %s", grpcLocalTarget)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	//nolint:staticcheck // grpc.DialContext is deprecated upstream; tracked for
	// migration to grpc.NewClient in a follow-up task.
	conn, err := grpc.DialContext(
		ctx,
		grpcLocalTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("[gateway] dial gRPC backend: %v", err)
	}
	defer conn.Close()

	gwMux := runtime.NewServeMux()
	if err := pb.RegisterBankaServiceHandlerClient(
		ctx,
		gwMux,
		pb.NewBankaServiceClient(conn),
	); err != nil {
		log.Fatalf("[gateway] register handler client: %v", err)
	}
	if err := pbactuary.RegisterActuaryServiceHandlerClient(
		ctx,
		gwMux,
		pbactuary.NewActuaryServiceClient(conn),
	); err != nil {
		log.Fatalf("[gateway] register actuary handler client: %v", err)
	}

	// Kombinovani HTTP mux: gRPC-Gateway + direktni HTTP handleri.
	httpMux := http.NewServeMux()
	httpMux.Handle("GET /bank/admin/exchanges/test-mode", marketModeHTTPHandler)                           // GET /bank/admin/exchanges/test-mode
	httpMux.Handle("/bank/payments/", receiptHandler)                                                      // GET /bank/payments/{id}/receipt
	httpMux.Handle("/bank/client/exchange-transfers", exchangeTransferHandler)                             // POST /bank/client/exchange-transfers
	httpMux.Handle("/bank/exchange-rates", exchangeRateHandler)                                            // GET /bank/exchange-rates[?from=X&to=Y&amount=Z]
	httpMux.Handle("/bank/exchange-rates/execute", exchangeRateHandler)                                    // POST /bank/exchange-rates/execute
	httpMux.Handle("POST /bank/cards/request", karticaRequestHandler)                                      // POST /bank/cards/request (Flow 2 Korak 1)
	httpMux.Handle("POST /bank/cards/confirm", karticaRequestHandler)                                      // POST /bank/cards/confirm (Flow 2 Korak 2)
	httpMux.Handle("GET /bank/cards/my", klientKarticeHandler)                                             // GET  /bank/cards/my (klijentske kartice)
	httpMux.Handle("PATCH /bank/cards/{id}/block", klientKarticeHandler)                                   // PATCH /bank/cards/{id}/block (blokiranje)
	httpMux.Handle("/bank/internal/actuary/", internalActuaryHandler)                                      // POST/DELETE — user-service interni pozivi
	httpMux.Handle("GET /bank/trading/my-orders", myOrdersHandler)                                         // GET /bank/trading/my-orders — caller's own orders (all roles)
	httpMux.Handle("POST /bank/trading/fx-breakdown", tradingFXHandler)                                    // POST /bank/trading/fx-breakdown — FX transparency za klijente
	httpMux.Handle("/bank/portfolio/", portfolioHandler)                                                   // GET /bank/portfolio/my, POST /bank/portfolio/publish, POST /bank/portfolio/exercise
	httpMux.Handle("/bank/tax/", taxHandler)                                                               // GET /bank/tax/users, POST /bank/tax/calculate
	httpMux.Handle("/bank/funds/", fundHandler)                                                            // GET /bank/funds, POST /bank/funds/{id}/invest, POST /bank/funds/{id}/withdraw
	httpMux.Handle("/bank/funds", fundHandler)                                                             // GET /bank/funds (without trailing slash)
	httpMux.Handle("GET /bank/bank-accounts", http.HandlerFunc(investmentFundHandler.BankAccountsHandler)) // GET /bank/bank-accounts — RSD računi banke (supervizor/admin)
	httpMux.Handle("/bank/investment-funds/orders", investmentFundHandler)                                 // POST /bank/investment-funds/orders — nalog za kupovinu za fond
	httpMux.Handle("/bank/investment-funds/", investmentFundHandler)                                       // GET /bank/investment-funds/{id} — detalj fonda
	httpMux.Handle("/bank/investment-funds", investmentFundHandler)                                        // GET (discovery) + POST (kreiranje) fonda
	// OTC (Faza 2) — Faza 2 spec: /api/otc/offers...
	httpMux.Handle("/api/otc/marketplace", otcHandler) // GET — public_shares marketplace (Faza 2)
	httpMux.Handle("/api/otc/offers", otcHandler)      // POST (create) + GET (list)
	httpMux.Handle("/api/otc/offers/", otcHandler)     // GET /{id}, PATCH /{id}/{counter|accept|decline}
	// OTC Contracts & SAGA (Celina 4) — /api/otc/contracts...
	httpMux.Handle("/api/otc/contracts", otcContractHandler)  // GET — lista ugovora korisnika
	httpMux.Handle("/api/otc/contracts/", otcContractHandler) // GET /{id}, POST /{id}/execute

	// ── Interbank si-tx-proto (POST /interbank, GET /public-stock, /negotiations, /user) ─
	// Dolazni protokol-ranog tipa endpointi — prima ih druga banka. X-Api-Key auth.
	httpMux.Handle("POST /interbank", http.HandlerFunc(interbankProtoHandler.HandleInterbank))
	httpMux.Handle("GET /public-stock", http.HandlerFunc(interbankProtoHandler.HandlePublicStock))
	httpMux.Handle("/negotiations", http.HandlerFunc(interbankProtoHandler.HandleNegotiations))
	httpMux.Handle("/negotiations/", http.HandlerFunc(interbankProtoHandler.HandleNegotiations))
	httpMux.Handle("/user/", http.HandlerFunc(interbankProtoHandler.HandleUserInfo))

	// ── Interbank klijentski endpoint-i (JWT) ────────────────────────────────
	httpMux.Handle("/bank/interbank/payments", interbankClientHandler)
	httpMux.Handle("/bank/interbank/public-stocks", interbankClientHandler)
	httpMux.Handle("/bank/interbank/negotiations", interbankClientHandler)
	httpMux.Handle("/bank/interbank/negotiations/", interbankClientHandler)
	httpMux.Handle("/bank/interbank/contracts", interbankClientHandler)
	httpMux.Handle("/bank/interbank/contracts/", interbankClientHandler)
	// Bank Profit Portal (Celina 4) — supervisor-only analytics
	httpMux.Handle("GET /bank/actuary-performance", http.HandlerFunc(bankProfitHandler.ActuaryPerformanceHandler))
	httpMux.Handle("GET /bank/fund-positions", http.HandlerFunc(bankProfitHandler.FundPositionsHandler))

	httpMux.Handle("/", gwMux) // sve ostalo → gRPC-Gateway

	// Root HTTP mux: /metrics ide direktno na Prometheus handler (bez middleware-a
	// koji bi nadkomplikovao scrape); sav ostali saobraćaj se instrumentira.
	rootMux := http.NewServeMux()
	rootMux.Handle("/metrics", metrics.Handler())
	rootMux.Handle("/", metrics.HTTPMiddleware(httpMux))

	gatewaySrv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      rootMux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// ── 7. Start InstallmentWorker (cron job) ────────────────────────────────
	// Worker koristi isti ctx koji se otkazuje pri SIGINT/SIGTERM,
	// što garantuje graceful shutdown bez dodatne sinhronizacije.
	go installmentWorker.Start(ctx)

	// ── 7e. Start TradingEngine (async order execution + fund settlement) ─────
	go tradingEngine.Start(ctx)

	// ── 7e2. Daily-ish scan: PENDING futures orders past settlement → DECLINED ─
	futuresExpiryWorker := worker.NewFuturesPendingExpiryWorker(orderRepo, listingRepo, tradingService)
	go futuresExpiryWorker.Start(ctx)

	// ── 7e3. Daily scan: VALID OTC contracts past settlement_date → EXPIRED ───
	otcContractExpiryWorker := worker.NewOTCContractExpiryWorker(otcRepo)
	go otcContractExpiryWorker.Start(ctx)

	// ── 7c. Start ListingRefresherWorker (osvežava cene hartija periodično) ────
	listingRefreshInterval := time.Duration(cfg.ListingRefreshIntervalMinutes) * time.Minute
	listingRefresherWorker := worker.NewListingRefresherWorker(listingRepo, listingRefreshInterval, cfg.EODHDAPIKey, cfg.FinnhubAPIKey, cfg.AlphaVantageAPIKey, cfg.ListingRequireLiveQuotes, tickBus)
	go listingRefresherWorker.Start(ctx)

	// ── 7d. Start DailyLimitResetWorker (resetuje used_limit agenata u 23:59) ─
	dailyLimitResetWorker := worker.NewDailyLimitResetWorker(actuaryService)
	go dailyLimitResetWorker.Start(ctx)

	// ── 7f. Start MonthlyTaxWorker (obračunava + naplaćuje porez 1. u mesecu) ─
	// Adapter pretvara (year, month) u kalendarski prozor i loguje summary.
	// Držimo ga ovde (a ne u worker paketu) kako worker ne bi uvozio service
	// paket — izbegavamo cyclic import sa listing_service.go.
	taxRunner := func(ctx context.Context, year int, month time.Month) error {
		start, end := service.MonthWindow(year, month, time.Local)
		summary, err := taxService.CalculateAndCollectForPeriod(ctx, start, end, service.TaxTriggeredByCron)
		if err != nil {
			return err
		}
		log.Printf("[main] MonthlyTaxWorker %d-%02d done — processed=%d collected=%.2f RSD (full=%d partial=%d unpaid=%d errors=%d)",
			year, int(month), summary.ProcessedUsers, summary.TotalCollectedF64,
			summary.FullyCollected, summary.Partial, summary.Unpaid, summary.Errors)
		return nil
	}
	monthlyTaxWorker := worker.NewMonthlyTaxWorker(taxRunner)
	go monthlyTaxWorker.Start(ctx)

	// ── 7h. Start InterbankRetryWorker (slanje neuspelih poruka drugoj banci) ─
	go interbankMsgSvc.RunRetryLoop(ctx, time.Duration(cfg.InterbankRetryBackoffSeconds)*time.Second)

	// ── 7g. Start FundPerformanceWorker (daily snapshot fonda u ponoć) ────────
	// Adapter dohvata sve fondove, za svaki traži FundValueRSD iz servisa, i
	// radi UPSERT u fund_performance_snapshots (ON CONFLICT DO UPDATE).
	snapshotRunner := func(ctx context.Context, date time.Time) error {
		funds, err := investmentFundService.ListFunds(ctx, domain.FundFilter{})
		if err != nil {
			return err
		}
		for _, f := range funds {
			details, err := investmentFundService.GetFundDetails(ctx, f.ID)
			if err != nil || details == nil {
				log.Printf("[snapshotRunner] skipping fund %d: %v", f.ID, err)
				continue
			}
			var totalInvested float64
			db.WithContext(ctx).Raw(
				`SELECT COALESCE(SUM(invested_rsd), 0) FROM core_banking.fund_positions WHERE fund_id = ?`,
				f.ID,
			).Scan(&totalInvested)

			if err := db.WithContext(ctx).Exec(`
				INSERT INTO core_banking.fund_performance_snapshots
					(fund_id, snapshot_date, fund_value_rsd, total_invested, liquid_assets)
				VALUES (?, ?, ?, ?, ?)
				ON CONFLICT (fund_id, snapshot_date) DO UPDATE SET
					fund_value_rsd = EXCLUDED.fund_value_rsd,
					total_invested = EXCLUDED.total_invested,
					liquid_assets  = EXCLUDED.liquid_assets
			`, f.ID, date, details.FundValueRSD, totalInvested, details.LiquidAssets).Error; err != nil {
				log.Printf("[snapshotRunner] upsert fund %d: %v", f.ID, err)
			}
		}
		return nil
	}
	fundPerfWorker := worker.NewFundPerformanceWorker(snapshotRunner)
	go fundPerfWorker.Start(ctx)

	// ── 7b. Start ActuaryConsumer (RabbitMQ event listener) ──────────────────
	// Listens on the user_created queue and auto-provisions actuary profiles
	// for SUPERVISOR and AGENT employees created in user-service.
	if cfg.RabbitMQURL != "" {
		go worker.StartActuaryConsumer(ctx, cfg.RabbitMQURL, actuaryRepo)
	} else {
		log.Printf("[main] RABBITMQ_URL nije postavljen — ActuaryConsumer neće biti pokrenut")
	}

	// ── 8. Start gRPC server ─────────────────────────────────────────────────
	go func() {
		if err := grpcSrv.Serve(); err != nil {
			log.Fatalf("[grpc] serve error: %v", err)
		}
	}()

	// ── 9. Start gRPC-Gateway HTTP server ────────────────────────────────────
	go func() {
		log.Printf("[gateway] HTTP listening on %s → gRPC %s", cfg.HTTPAddr, grpcLocalTarget)
		if err := gatewaySrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[gateway] ListenAndServe error: %v", err)
		}
	}()

	// ── 9. Graceful shutdown on SIGINT / SIGTERM ──────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("[main] shutdown signal received")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := gatewaySrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[gateway] shutdown error: %v", err)
	}

	grpcSrv.Stop()
	log.Println("[main] clean shutdown complete")
}
