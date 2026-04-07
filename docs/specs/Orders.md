# Order (nalog)

Orderi su nalozi koje kreiramo kada želimo da kupimo ili prodamo bilo koju hartiju od vrednosti.

**Specifikacija obuhvata:**
- 4 osnovna tipa naloga: **Market**, **Limit**, **Stop**, **Stop-Limit**
- 2 načina izvršenja: **All or None (AON)** i **Margin** (korišćenje kredita)

Svaki order može biti **BUY** ili **SELL**:
- **BUY** – kada postavljamo order da **kupimo** hartiju (u Portalu "Hartije od vrednosti")
- **SELL** – kada postavljamo order da **prodamo** hartiju (u Portalu "Moj portfolio")

---

## Entitet (struktura ordera)

| Podatak            | Opis                                                                                  | Primer                     | Učestalost promena                      |
|--------------------|---------------------------------------------------------------------------------------|----------------------------|------------------------------------------|
| **User**           | Aktuar koji je postavio Order                                                         | Miloš Milošević            | Ne menja se                              |
| **Asset**          | Hartija od vrednosti koja se kupuje/prodaje                                           | 2                          | Ne menja se                              |
| **OrderType**      | Tip ordera                                                                            | Market, Limit, Stop        | Ne menja se                              |
| **Quantity**       | Količina hartija od vrednosti po ugovoru                                              | 10                         | Ne menja se                              |
| **Contract Size**  | Broj ugovora                                                                          | 1                          | Ne menja se                              |
| **Price Per Unit** | Cena po jedinici za Limit i Stop-Limit naloge. Za Market naloge – referentna cena.    | 50$                        | Može varirati (ako korisnik menja nalog) |
| **Direction**      | Buy ili Sell                                                                          | Buy, Sell                  | Ne menja se                              |
| **Status**         | Status ordera                                                                         | Pending, Approved, Declined| Menja supervizor                         |
| **Approved By**    | Supervizor koji je odobrio/odbio (ili "No need for approval")                         | Nina Nikolić               | Ne menja se                              |
| **isDone**         | Da li je order u potpunosti završen                                                   | True, False                | Menja sistem                             |
| **Last Modification** | Datum i vreme poslednje modifikacije ordera                                        | 2022-04-28 16:00:00        | Menja sistem                             |
| **Remaining Portions** | Broj preostalih delova ordera koji nisu izvršeni                                  | 4                          | Menja se                                 |
| **After Hours**    | Da li je order kreiran manje od 4 sata od zatvaranja berze                            | True, False                | Ne menja se                              |

---

## Atribut `Status`

Order je potrebno odobriti ako ga je kreirao **Agent** i važi nešto od sledećeg:
- Agent ima flag `Need Approval = true`
- Agent je iskoristio svoj dnevni limit
- Ukupna trenutna cena ordera prelazi agentov dnevni limit

> Kod hartija sa `settlement date` koje su istekle – postoji samo opcija **Decline** (sistem može automatski odbiti takav nalog).

**Supervizor može** samo **jednom** promeniti status u `Approved` ili `Declined` (na Portalu za pregled ordera).

> **Napomena:** Klijentovi orderi su automatski **Approved**.

---

## Atribut `approvedBy`

Ako nije bilo potrebe za odobrenjem, vrednost je: `"No need for approval"`.

## Atribut `lastModification`

Ovde se beleže: odobravanje, odbijanje, završetak naloga i sl.

---

## Tipovi naloga

### Market Order

Izvršava se **odmah** po trenutnoj tržišnoj ceni.

- **Cena pri kupovini** = `Contract Size × Ask`
- **Cena pri prodaji** = `Contract Size × Bid`

> `Contract Size`, `Ask` i `Bid` su podaci hartije za koju kreiramo order.

**Provizija:** 14% od približne cene ordera **ili** 7$ – uzima se **manji** iznos.  
Provizija se prebacuje na bankin račun u istoj valuti.

> Ako korisnik unese **samo broj hartija** (bez limita/stopa) – podrazumeva se **Market Order**.

---

### Limit Order

Izvršava se **samo ako je trenutna cena povoljnija** od zadatog limita.

- **Buy Limit Order** (kupovina): izvršava se ako je `Ask ≤ limit`  
  `Cena kupovine = Contract Size × min(limit, ask)`

- **Sell Limit Order** (prodaja): izvršava se ako je `Bid ≥ limit`  
  `Cena prodaje = Contract Size × max(limit, bid)`

**Provizija:** 24% od početne cene ordera **ili** 12$ – uzima se **manji** iznos.

> Ako korisnik unese `Limit Value` – podrazumeva se **Limit Order**.

---

### Stop Order (Stop-Loss)

Order se **ne izvršava odmah**; kada tržišna cena dostigne `Stop` vrednost, pretvara se u **Market Order**.

- **Buy Stop Order**: izvršava se kada `Ask` postane **veći** od stop vrednosti  
- **Sell Stop Order**: izvršava se kada `Bid` postane **manji** od stop vrednosti  

Cene se računaju isto kao za Market Order (`Contract Size × Ask` / `× Bid`).

> Ako korisnik unese `Stop Value` – podrazumeva se **Stop Order**.

---

### Limit VS Stop Order

| Karakteristika       | Limit Order                                      | Stop Order                                       |
|----------------------|--------------------------------------------------|--------------------------------------------------|
| Svrha                | Kupi po max ceni / proda po min ceni             | Zaštita od gubitka ili ulazak pri proboju        |
| Aktivacija           | Odmah (ako je uslov ispunjen)                    | Tek kada tržišna cena dostigne stop nivo         |
| Postaje              | Limit Order (ostaje limit)                       | Market Order (nakon aktivacije)                  |
| Vidljivost           | Vidljiv drugim trgovcima                         | Nije vidljiv dok se ne aktivira                  |
| Garancija izvršenja  | Ne (može ostati neispunjen)                      | Da (posle aktivacije, ali po trenutnoj ceni)     |

**Primeri:**
- **Limit Buy** – kupi po 30 RSD ili niže
- **Limit Sell** – proda po 50 RSD ili više
- **Stop Buy** – kupi ako cena predje 120 USD (tržišno)
- **Stop Sell** – prodaj ako cena padne ispod 90 USD (tržišno)

> Ako korisnik ne unese ni Limit ni Stop vrednost → **Market Order**.  
> Preporučena napomena:  
> *"If you leave limit/stop fields empty, market price is taken into account."*

---

### Stop-Limit Order

Kombinacija Stop i Limit naloga.  
Kada tržišna cena dostigne **Stop** vrednost, order se pretvara u **Limit Order** (ne u Market).

- **Stop** – cena koja aktivira order  
- **Limit** – maksimalna (kupovina) / minimalna (prodaja) cena nakon aktivacije

- **Buy Stop-Limit**: kada `Ask ≥ Stop` → pretvara se u Buy Limit Order sa zadatim limitom  
- **Sell Stop-Limit**: kada `Bid ≤ Stop` → pretvara se u Sell Limit Order sa zadatim limitom

> Korisno kada želite zaštitu, ali **ne želite da platite bilo koju cenu**.

---

## Načini izvršenja naloga

### All or None (AON)

Može se uključiti za bilo koji tip ordera.  
Order se izvršava **samo u celini**, nikako delimično.

### Margin

Omogućava korišćenje **kredita** za izvršenje naloga.  
Permisije:
- Zaposleni mora imati permisiju
- Klijent sa odobrenim kreditom dobija automatski

Uslovi za prihvatanje (mora biti zadovoljen **bar jedan**):
1. Klijent: kredit `>` `Initial Margin Cost`
2. Klijent ili aktuar: sredstva na odabranom računu `>` `Initial Margin Cost`

> `Initial Margin Cost = Maintenance Margin (hartije) × 1.1`

---

## Create Orders (kreiranje naloga)

- **BUY** – preko Portala "Hartije od vrednosti" (unosi se broj hartija, inicijalno 1)  
- **SELL** – preko Portala "Moj portfolio" (bira se količina akcija)

**Uvek tražiti potvrdu** pre kreiranja.

Dialog treba da sadrži:
- koliko hartija
- tip ordera
- **približnu cenu**:  
  `Approximate Price = Contract Size × Price Per Unit × Quantity`

`Price Per Unit` zavisi od tipa:
- Market → `Price/Quote`
- Limit / Stop-Limit → `Limit Value`
- Stop → `Stop Value`

### Izbor računa pri kupovini

- **Klijent** – sa njegovog računa (konverzija sa provizijom)
- **Zaposleni (aktuar)** – sa bankinog računa (konverzija **bez** provizije)

---

## Simulacija izvršavanja ordera (deo po deo)

Pošto nemamo pristup orderbook-u, koristi se sledeća simulacija:

- Svaki deo ordera izvršava se nakon **slučajnog vremenskog intervala**:

Interval (sekundi) = Random(0, 24 * 60 / (Volume / PreostalaKolicina))

- Cena se računa prema formuli za dati tip ordera (Market/Limit/Stop/Stop-Limit).

**Primer (kupovina 10 akcija):**
1. Izaberi slučajan broj akcija (1–10) → `n`
2. Zabeleži transakciju za `n` akcija po važećoj ceni
3. Ostatak = 10–n, ponavljaj dok se ne kupi svih 10

Order se može ispuniti od **različitih prodavaca** (osim za AON – sve ili ništa).

### After-hours i zatvorena berza

- Ako je berza **zatvorena** → obavestiti korisnika
- Ako je berza u **after-hours** (manje od 4 sata od zatvaranja) → svaki deo ordera se izvršava **dodatno sporije** (npr. +30 minuta po delu)
