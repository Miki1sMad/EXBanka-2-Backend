# Backend API – vodič za frontend

User-service izlaže **HTTP REST API** preko gRPC-Gateway-a. Svi zahtevi i odgovori su **JSON**, polja u **snake_case**.

---

## Base URL

| Okruženje | URL |
|-----------|-----|
| Docker (default) | `http://localhost:8082` |
| Lokalno (bez Dockera) | `http://localhost:8080` |

**Napomena:** Notification-service se ne poziva direktno sa frontenda — mejlovi (aktivacija, reset lozinke) šalju se automatski kada user-service objavi događaj u RabbitMQ.

---

## Zaglavlja

- **Content-Type:** `application/json` (za sve POST/PUT sa telom)
- **Authorization:** `Bearer <access_token>` — obavezno za **zaštićene** rute (vidi tabelu ispod)

---

## Autentifikacija

- **Javne rute** — bez `Authorization` zaglavlja: `/health`, `/login`, `/refresh-token`, `/auth/set-password`, `/activate`, `/auth/forgot-password`, `/auth/reset-password`.
- **Zaštićene rute** — obavezno `Authorization: Bearer <access_token>`: `/permissions`, `/employee`, `/employee/{id}` (GET, POST, PUT), `/employee/{id}/active` (PATCH).

Access token dobijaš iz odgovora **POST /login** ili **POST /refresh-token**. Kad istekne, front treba da pošalje **POST /refresh-token** sa `refresh_token` i da zameni stari access token novim.

---

## Spisak ruta

### 1. Health check (javno)

| Metoda | Putanja | Auth | Opis |
|--------|---------|------|------|
| GET | `/health` | Ne | Provera da li servis radi |

**Response 200:**
```json
{ "status": "SERVING" }
```

---

### 2. Login (javno)

| Metoda | Putanja | Auth | Opis |
|--------|---------|------|------|
| POST | `/login` | Ne | Email + lozinka → access i refresh token |

**Request body:**
```json
{
  "email": "string",
  "password": "string"
}
```

**Response 200:**
```json
{
  "access_token": "string",
  "refresh_token": "string",
  "token_type": "Bearer",
  "expires_in": 3600
}
```

**Greške:** 401 ako su kredencijali pogrešni ili nalog neaktivan; 404 ako email ne postoji.

---

### 3. Refresh token (javno)

| Metoda | Putanja | Auth | Opis |
|--------|---------|------|------|
| POST | `/refresh-token` | Ne | Refresh token → novi access token |

**Request body:**
```json
{
  "refresh_token": "string"
}
```

**Response 200:**
```json
{
  "access_token": "string",
  "refresh_token": "string",
  "token_type": "Bearer",
  "expires_in": 3600
}
```

Refresh token se vraća isti (nema rolling session).

---

### 4. Forgot password (javno)

| Metoda | Putanja | Auth | Opis |
|--------|---------|------|------|
| POST | `/auth/forgot-password` | Ne | Šalje link za reset na email (uvek 200 da ne bi bilo user enumeration) |

**Request body:**
```json
{
  "email": "string"
}
```

**Response 200:**
```json
{
  "message": "If your email is registered in our system, you will receive a password reset link."
}
```

---

### 5. Reset password (javno)

| Metoda | Putanja | Auth | Opis |
|--------|---------|------|------|
| POST | `/auth/reset-password` | Ne | JWT iz email linka + nova lozinka (min 8 karaktera) |

**Request body:**
```json
{
  "token": "string",
  "new_password": "string"
}
```

**Response 200:**
```json
{
  "message": "Password reset successfully."
}
```

---

### 6. Set password (javno – aktivacija)

| Metoda | Putanja | Auth | Opis |
|--------|---------|------|------|
| POST | `/auth/set-password` | Ne | JWT aktivacije iz email linka + lozinka (min 8 karaktera) |

**Request body:**
```json
{
  "token": "string",
  "password": "string"
}
```

**Response 200:**
```json
{
  "message": "Password set successfully."
}
```

---

### 7. Activate account (javno)

| Metoda | Putanja | Auth | Opis |
|--------|---------|------|------|
| POST | `/activate` | Ne | Token iz linka + nova lozinka + potvrda. Pravila: min 8, max 32 karaktera; min 2 cifre, 1 veliko, 1 malo slovo |

**Za frontend:** Korisnik dolazi na **frontend** URL iz emaila: `FRONTEND_URL/activate?token=<jwt>`. Frontend mora da ima **stranicu na ruti `/activate`** koja: (1) učita `token` iz query parametra, (2) prikaže formu za novu lozinku i potvrdu, (3) na submit pošalje **POST na backend** (Base URL iz tabele gore) na putanju **`/activate`** sa telom ispod. Bez ove stranice na frontu korisnik dobija „stranica nije pronađena“.

**Request body:**
```json
{
  "token": "string",
  "new_password": "string",
  "confirm_password": "string"
}
```

**Response 200:**
```json
{
  "message": "Account activated successfully."
}
```

---

### 8. Get all permissions (zaštićeno – Admin)

| Metoda | Putanja | Auth | Opis |
|--------|---------|------|------|
| GET | `/permissions` | Da (Admin) | Lista svih permission kodova za forme (Create/Edit zaposleni) |

**Response 200:**
```json
{
  "permissions": [
    { "id": 1, "permission_code": "MANAGE_USERS" },
    { "id": 2, "permission_code": "..." }
  ]
}
```

---

### 9. Get all employees (zaštićeno – Admin)

| Metoda | Putanja | Auth | Opis |
|--------|---------|------|------|
| GET | `/employee` | Da (Admin) | Lista zaposlenih sa filterima i paginacijom |

**Query parametri (svi opcioni):**

| Parametar | Tip | Opis |
|-----------|-----|------|
| `email` | string | Delimično poklapanje, case-insensitive; prazan = bez filtera |
| `first_name` | string | Delimično poklapanje |
| `last_name` | string | Delimično poklapanje |
| `position` | string | Delimično poklapanje |
| `page` | int32 | Strana (1-based); 0 tretira se kao 1 |
| `page_size` | int32 | Broj po strani; 0 tretira se kao 10 |

**Response 200:**
```json
{
  "employees": [
    {
      "user": {
        "id": 1,
        "email": "string",
        "first_name": "string",
        "last_name": "string",
        "birth_date": 0,
        "gender": 1,
        "phone_number": "string",
        "address": "string",
        "user_type": 2,
        "is_active": true,
        "created_at": "2025-01-01T00:00:00Z"
      },
      "username": "string",
      "position": "string",
      "department": "string",
      "permissions": ["MANAGE_USERS", "..."]
    }
  ]
}
```

---

### 10. Get employee by ID (zaštićeno – Admin ili MANAGE_USERS)

| Metoda | Putanja | Auth | Opis |
|--------|---------|------|------|
| GET | `/employee/{id}` | Da | Jedan zaposleni po ID-u (za edit formu). Admin ne može biti izmenjen (PERMISSION_DENIED). |

**Path:** `id` — number (user ID).

**Response 200:**
```json
{
  "employee": {
    "user": { ... },
    "username": "string",
    "position": "string",
    "department": "string",
    "permissions": ["MANAGE_USERS"]
  }
}
```

**Greške:** 404 ako ID ne postoji; 403 ako je target Admin.

---

### 11. Create employee (zaštićeno – Admin)

| Metoda | Putanja | Auth | Opis |
|--------|---------|------|------|
| POST | `/employee` | Da (Admin) | Kreira zaposlenog bez lozinke; šalje se aktivacioni email |

**Request body:**
```json
{
  "email": "string",
  "first_name": "string",
  "last_name": "string",
  "birth_date": 0,
  "gender": 1,
  "phone_number": "string",
  "address": "string",
  "username": "string",
  "position": "string",
  "department": "string",
  "is_active": true,
  "permissions": ["MANAGE_USERS"]
}
```

- `birth_date`: Unix timestamp u **milisekundama**.
- `gender`: vrednost iz enuma (0–3), vidi ispod.
- `is_active`: opciono; ako se izostavi, default je true.
- `permissions`: niz kodova (npr. `"MANAGE_USERS"`).

**Response 200:**
```json
{
  "id": 1,
  "email": "user@example.com"
}
```

---

### 12. Update employee (zaštićeno – Admin ili MANAGE_USERS)

| Metoda | Putanja | Auth | Opis |
|--------|---------|------|------|
| PUT | `/employee/{id}` | Da | Menja sva mutable polja i ceo set permisija. Admin ne može biti izmenjen. |

**Path:** `id` — number (user ID).

**Request body:**
```json
{
  "id": 1,
  "email": "string",
  "first_name": "string",
  "last_name": "string",
  "birth_date": 0,
  "gender": 1,
  "phone_number": "string",
  "address": "string",
  "position": "string",
  "department": "string",
  "is_active": true,
  "permissions": ["MANAGE_USERS"]
}
```

- `id` u body-u mora da odgovara `{id}` u putanji.
- `permissions` zamenjuje ceo prethodni set.

**Response 200:**
```json
{
  "employee": {
    "user": { ... },
    "username": "string",
    "position": "string",
    "department": "string",
    "permissions": ["..."]
  }
}
```

**Greške:** 404 ako ID ne postoji; 403 ako je target Admin.

---

### 12.1 Toggle employee active (zaštićeno – Admin ili MANAGE_USERS)

| Metoda | Putanja | Auth | Opis |
|--------|---------|------|------|
| PATCH | `/employee/{id}/active` | Da | Postavlja `is_active` za bilo kog zaposlenog ili administratora (bez potrebe za punim edit flow-om). |

**Path:** `id` — number (user ID).

**Request body:**
```json
{
  "is_active": false
}
```

**Response 200:**
```json
{
  "is_active": false
}
```

**Greške:** 404 ako ID ne postoji; 403 ako caller nema ADMIN ili MANAGE_USERS, ili ako je target CLIENT.

**Napomena:** Da bi PATCH ruta bila dostupna: obrišite `proto/user/toggle_types_stub.go` i u root-u backend repoa pokrenite `make proto`. Inače backend se kompajlira, ali HTTP ruta za toggle nije registrovana (gRPC metoda postoji).

---

## Enumi (JSON vrednosti)

### UserType (`user_type`)

| Vrednost | Broj | Opis |
|----------|------|------|
| `USER_TYPE_UNSPECIFIED` | 0 | |
| `USER_TYPE_ADMIN` | 1 | Admin |
| `USER_TYPE_EMPLOYEE` | 2 | Zaposleni |
| `USER_TYPE_CLIENT` | 3 | Klijent |

### Gender (`gender`)

| Vrednost | Broj |
|----------|------|
| `GENDER_UNSPECIFIED` | 0 |
| `GENDER_MALE` | 1 |
| `GENDER_FEMALE` | 2 |
| `GENDER_OTHER` | 3 |

U JSON-u backend može primati i brojeve (0–3) za enume.

---

## Format grešaka (gRPC-Gateway)

Pri 4xx/5xx gateway često vraća JSON u formatu:

```json
{
  "code": 5,
  "message": "opis greške"
}
```

`code` je gRPC status kod (npr. 5 = NOT_FOUND, 7 = PERMISSION_DENIED, 16 = UNAUTHENTICATED). Front može da mapira kodove na poruke ili da prikaže `message`.

---

## Email (Gmail SMTP) – backend integracija

Mejlovi (aktivacija, reset lozinke) šalju se preko **notification-service** kada **user-service** objavi događaj u RabbitMQ. Primaoc uvvek dolazi iz payload-a (email sa frontenda / zahteva), nikad nije hardkodovan.

**Env varijable** (npr. u `.env` ili u okruženju):

- `SMTP_HOST` (npr. `smtp.gmail.com`)
- `SMTP_PORT` (npr. `587`, STARTTLS)
- `SMTP_USER` (Gmail adresa ili app nalog)
- `SMTP_PASS` (Gmail app password)
- `FROM_EMAIL` (pošiljalac)
- `FRONTEND_URL` (baza za linkove, npr. `http://localhost:3001`)

**Linkovi u mejlovima:**

- Aktivacija: `FRONTEND_URL/activate?token=<token>`
- Reset lozinke: `FRONTEND_URL/reset-password?token=<token>`

**Primer payload-a koji stiže u notification-service** (iz RabbitMQ; `email` je primaoc sa frontenda):

```json
{
  "type": "ACTIVATION",
  "email": "korisnik@example.com",
  "token": "eyJhbGciOiJIUzI1NiIs..."
}
```

Za reset lozinke frontend šalje **POST /auth/forgot-password** sa `{"email": "korisnik@example.com"}`; user-service generiše token i objavljuje `{"type": "RESET", "email": "korisnik@example.com", "token": "..."}`. Notification-service šalje mejl na tu `email` adresu.

**Gde se šta nalazi u kodu:**

| Šta | Gde |
|-----|-----|
| Učitavanje env (.env opciono) | `services/notification-service/cmd/server/main.go` |
| Konfig (SMTP_*, FRONTEND_URL) | `services/notification-service/internal/config/config.go` |
| Reusable SMTP slanje (STARTTLS, log bez lozinke) | `services/notification-service/internal/smtp/sender.go` |
| Šabloni i tip mejla (ACTIVATION/RESET/CONFIRMATION) | `services/notification-service/internal/service/notification_service.go` |
| RabbitMQ consumer → SendEmail(event) | `services/notification-service/internal/transport/rabbitmq_consumer.go` |
| Objavljivanje događaja (email iz zahteva) | user-service: `internal/handler/grpc_handler.go`, `internal/utils/rabbitmq.go` |

---

## Rezime za frontend

1. **Base URL:** `http://localhost:8082` (Docker) ili `http://localhost:8080` (lokalno).
2. **Content-Type:** `application/json` za sve POST/PUT sa telom.
3. **Authorization:** `Bearer <access_token>` za sve rute osim: `/health`, `/login`, `/refresh-token`, `/auth/set-password`, `/activate`, `/auth/forgot-password`, `/auth/reset-password`.
4. **JSON:** sva polja u **snake_case** (npr. `first_name`, `birth_date`, `user_type`).
5. **Tokeni:** nakon logina čuvaj `access_token` i `refresh_token`; pri 401 pošalji **POST /refresh-token** sa `refresh_token`, pa ponovi zahtev sa novim `access_token`.
6. **Notification-service:** ne poziva se sa frontenda; mejlovi se šalju automatski (aktivacija, reset lozinke).
