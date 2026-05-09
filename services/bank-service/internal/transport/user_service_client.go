// Package transport — outbound gRPC connections from bank-service.
package transport

import (
	"context"
	"fmt"

	userv1 "banka-backend/proto/user"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// UserServiceClient wraps the generated gRPC stub so the rest of bank-service
// depends on this thin abstraction, not on the generated proto types directly.
type UserServiceClient struct {
	client userv1.UserServiceClient
	conn   *grpc.ClientConn
}

// NewUserServiceClient dials user-service at addr and returns a ready client.
// The caller is responsible for calling Close() when done.
func NewUserServiceClient(addr string) (*UserServiceClient, error) {
	//nolint:staticcheck
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial user-service at %s: %w", addr, err)
	}
	return &UserServiceClient{
		client: userv1.NewUserServiceClient(conn),
		conn:   conn,
	}, nil
}

// GetClientEmail calls GetClientByID on user-service and returns the client's
// email address. The caller should use a context with an appropriate timeout.
//
// Returns the gRPC error as-is so the caller can inspect the status code:
//   - codes.NotFound      → client does not exist
//   - codes.DeadlineExceeded / codes.Unavailable → user-service unreachable
func (c *UserServiceClient) GetClientEmail(ctx context.Context, clientID int64) (string, error) {
	// Forward the Authorization header from the incoming request so user-service
	// interceptor can validate the same JWT that bank-service already verified.
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		ctx = metadata.NewOutgoingContext(ctx, md)
	}
	resp, err := c.client.GetClientByID(ctx, &userv1.GetClientByIDRequest{Id: clientID})
	if err != nil {
		return "", err
	}
	return resp.GetClient().GetEmail(), nil
}

// ClientInfo objedinjuje ime, prezime i email klijenta u jednoj strukturi.
// Vraća se metodom GetClientInfo kako bi se izbeglo višestruko pozivanje user-service-a.
type ClientInfo struct {
	FirstName string
	LastName  string
	Email     string
}

// GetClientInfo calls GetClientByID on user-service and returns the client's
// first name, last name, and email in a single round-trip.
//
// Returns the gRPC error as-is so the caller can inspect the status code:
//   - codes.NotFound      → client does not exist
//   - codes.DeadlineExceeded / codes.Unavailable → user-service unreachable
func (c *UserServiceClient) GetClientInfo(ctx context.Context, clientID int64) (*ClientInfo, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		ctx = metadata.NewOutgoingContext(ctx, md)
	}
	resp, err := c.client.GetClientByID(ctx, &userv1.GetClientByIDRequest{Id: clientID})
	if err != nil {
		return nil, err
	}
	cl := resp.GetClient()
	return &ClientInfo{
		FirstName: cl.GetFirstName(),
		LastName:  cl.GetLastName(),
		Email:     cl.GetEmail(),
	}, nil
}

// GetEmployeeInfo calls GetEmployeeByID on user-service and returns the
// employee's first name, last name and email. Used for actuary lookups where
// GetClientByID would return NOT_FOUND because the user is EMPLOYEE-type.
//
// Returns the gRPC error as-is so the caller can inspect the status code:
//   - codes.NotFound         → employee does not exist
//   - codes.PermissionDenied → caller lacks Admin / MANAGE_USERS / SUPERVISOR
func (c *UserServiceClient) GetEmployeeInfo(ctx context.Context, employeeID int64) (*ClientInfo, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		ctx = metadata.NewOutgoingContext(ctx, md)
	}
	resp, err := c.client.GetEmployeeByID(ctx, &userv1.GetEmployeeByIDRequest{Id: employeeID})
	if err != nil {
		return nil, err
	}
	u := resp.GetEmployee().GetUser()
	return &ClientInfo{
		FirstName: u.GetFirstName(),
		LastName:  u.GetLastName(),
		Email:     u.GetEmail(),
	}, nil
}

// GetAllEmployeesAsMap fetches all employees (including ADMIN-type users, which
// GetEmployeeByID refuses to return) and returns them keyed by user ID.
// Fetches up to 500 employees in one call — sufficient for any realistic bank.
func (c *UserServiceClient) GetAllEmployeesAsMap(ctx context.Context) (map[int64]*ClientInfo, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		ctx = metadata.NewOutgoingContext(ctx, md)
	}
	resp, err := c.client.GetAllEmployees(ctx, &userv1.GetAllEmployeesRequest{
		Page:     1,
		PageSize: 500,
	})
	if err != nil {
		return nil, err
	}
	m := make(map[int64]*ClientInfo, len(resp.GetEmployees()))
	for _, ep := range resp.GetEmployees() {
		u := ep.GetUser()
		m[u.GetId()] = &ClientInfo{
			FirstName: u.GetFirstName(),
			LastName:  u.GetLastName(),
			Email:     u.GetEmail(),
		}
	}
	return m, nil
}

// GetClientName calls GetClientByID on user-service and returns the client's
// first and last name. The caller should use a context with an appropriate timeout.
//
// Returns the gRPC error as-is so the caller can inspect the status code:
//   - codes.NotFound      → client does not exist
//   - codes.DeadlineExceeded / codes.Unavailable → user-service unreachable
func (c *UserServiceClient) GetClientName(ctx context.Context, clientID int64) (firstName, lastName string, err error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		ctx = metadata.NewOutgoingContext(ctx, md)
	}
	resp, err := c.client.GetClientByID(ctx, &userv1.GetClientByIDRequest{Id: clientID})
	if err != nil {
		return "", "", err
	}
	return resp.GetClient().GetFirstName(), resp.GetClient().GetLastName(), nil
}

// GetMyEmail calls GetMyProfile on user-service and returns the caller's own
// email address. Works with any authenticated JWT (CLIENT or EMPLOYEE).
// Use this from HTTP handlers where the token belongs to the calling user.
func (c *UserServiceClient) GetMyEmail(ctx context.Context) (string, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		ctx = metadata.NewOutgoingContext(ctx, md)
	}
	resp, err := c.client.GetMyProfile(ctx, &userv1.GetMyProfileRequest{})
	if err != nil {
		return "", err
	}
	return resp.GetEmail(), nil
}

// Close releases the underlying gRPC connection.
func (c *UserServiceClient) Close() error {
	return c.conn.Close()
}
