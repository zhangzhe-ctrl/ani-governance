package data

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"strings"
	"time"

	inferencev1 "github.com/zhangzhe-ctrl/ani-inference-service/api/inference/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

// InferenceClientConfig is controlled deployment configuration, never user
// input. Both the endpoint and exact server certificate DNS name are required.
type InferenceClientConfig struct {
	Address, CAFile, CertFile, KeyFile, ServerName string
	Timeout                                        time.Duration
}

func InferenceConfigFromEnv() (InferenceClientConfig, error) {
	c := InferenceClientConfig{Address: os.Getenv("ANI_INFERENCE_ADDR"), CAFile: os.Getenv("ANI_INFERENCE_CA_FILE"), CertFile: os.Getenv("ANI_INFERENCE_CERT_FILE"), KeyFile: os.Getenv("ANI_INFERENCE_KEY_FILE"), ServerName: os.Getenv("ANI_INFERENCE_SERVER_NAME"), Timeout: 3 * time.Second}
	if c.Address == "" {
		return c, nil
	}
	if strings.TrimSpace(c.Address) != c.Address || c.CAFile == "" || c.CertFile == "" || c.KeyFile == "" || c.ServerName == "" || strings.ContainsAny(c.ServerName, "*:/\\ \t\n") {
		return c, errors.New("inference client requires managed address, CA, certificate, key and exact server DNS name")
	}
	return c, nil
}

type InferenceClient struct {
	client  inferencev1.InferenceServiceManagerClient
	timeout time.Duration
}

func NewInferenceClient(c InferenceClientConfig) (*InferenceClient, func(), error) {
	if c.Address == "" || c.ServerName == "" || strings.TrimSpace(c.Address) != c.Address || strings.ContainsAny(c.ServerName, "*:/\\ \t\n") || c.Timeout <= 0 || c.Timeout > 3*time.Second {
		return nil, nil, errors.New("invalid inference client configuration")
	}
	certificate, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		return nil, nil, errors.New("inference client certificate unavailable")
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil || len(leaf.URIs) != 1 || leaf.URIs[0].String() != "spiffe://ani.internal/service/ani-governance" {
		return nil, nil, errors.New("inference client requires the registered Governance URI identity")
	}
	pem, err := os.ReadFile(c.CAFile)
	if err != nil {
		return nil, nil, errors.New("inference client CA unavailable")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, nil, errors.New("inference client CA contains no certificates")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{certificate}, ServerName: c.ServerName,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 {
				return errors.New("unverified inference server identity")
			}
			for _, name := range state.PeerCertificates[0].DNSNames {
				if name == c.ServerName {
					return nil
				}
			}
			return errors.New("inference server exact DNS SAN mismatch")
		},
	}
	connection, err := grpc.NewClient(c.Address, grpc.WithDisableServiceConfig(), grpc.WithDisableRetry(), grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		return nil, nil, errors.New("invalid inference client address")
	}
	return &InferenceClient{client: inferencev1.NewInferenceServiceManagerClient(connection), timeout: c.Timeout}, func() { _ = connection.Close() }, nil
}

func (c *InferenceClient) Create(ctx context.Context, request *inferencev1.CreateInferenceServiceRequest) (*inferencev1.OperationResponse, error) {
	call, cancel := context.WithTimeout(metadata.NewOutgoingContext(ctx, metadata.MD{}), c.timeout)
	defer cancel()
	return c.client.CreateInferenceService(call, request)
}

func (c *InferenceClient) Delete(ctx context.Context, request *inferencev1.DeleteInferenceServiceRequest) (*inferencev1.OperationResponse, error) {
	call, cancel := context.WithTimeout(metadata.NewOutgoingContext(ctx, metadata.MD{}), c.timeout)
	defer cancel()
	return c.client.DeleteInferenceService(call, request)
}
