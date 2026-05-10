# EXBanka-2-Backend
## Implemented Services

### 1. User Service
* **Location:** `/services/user-service`
* **Responsibilities:** Employee CRUD, centralized authentication, and RBAC authorization.
* **Security:** JWT-based stateless sessions (Access, Refresh, Activation, Reset tokens). Includes mitigations for email enumeration and replay attacks.
* **Event Publisher:** Pushes asynchronous state-change payloads (e.g., password reset triggers) to the message broker.

### 2. Notification Service
* **Location:** `/services/notification-service`
* **Responsibilities:** Background worker for asynchronous email delivery.
* **Event Consumer:** Subscribes to the `email_notifications` queue.
* **Processing:** Parses AMQP payloads, renders MIME-compliant HTML templates (Activation, Reset, Confirmation), and dispatches via SMTP.
*odbrana*
