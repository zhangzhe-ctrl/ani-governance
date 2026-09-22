//go:build quota_lab

package quotalab

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	quotalabpb "go-wind-admin/api/gen/go/quota_lab/service/v1"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"
)

// GpuResourceReader 读取模拟资源（QUOTA-LAB-02 转发路径）。
type GpuResourceReader interface {
	GetResource(ctx context.Context, tenantID, resourceID string) (*quotalabpb.GetResourceResponse, error)
}

// GpuSimulatorAdapterConfig Governance → simulator 的 mTLS 客户端配置。
// simulator 只接受精确身份 ani-governance；TLS 最低 1.3（§11.3）。
type GpuSimulatorAdapterConfig struct {
	Address  string
	CAFile   string
	CertFile string
	KeyFile  string
	Timeout  time.Duration
}

// GpuSimulatorAdapterConfigFromEnv 从环境变量读取：
// ANI_QUOTA_SIMULATOR_ADDR / ANI_QUOTA_SIMULATOR_CA / _CERT / _KEY。
func GpuSimulatorAdapterConfigFromEnv() GpuSimulatorAdapterConfig {
	return GpuSimulatorAdapterConfig{
		Address:  os.Getenv("ANI_QUOTA_SIMULATOR_ADDR"),
		CAFile:   os.Getenv("ANI_QUOTA_SIMULATOR_CA"),
		CertFile: os.Getenv("ANI_QUOTA_SIMULATOR_CERT"),
		KeyFile:  os.Getenv("ANI_QUOTA_SIMULATOR_KEY"),
		Timeout:  3 * time.Second,
	}
}

// NewGpuSimulatorAdapter 构造 fail-closed mTLS 客户端；证书 SAN 必须精确为
// ani-governance。配置缺失返回错误（lab 装配在未配置时不注册 adapter）。
func NewGpuSimulatorAdapter(cfg GpuSimulatorAdapterConfig) (*GpuSimulatorAdapter, func(), error) {
	if cfg.Address == "" || cfg.Timeout <= 0 {
		return nil, nil, fmt.Errorf("gpu simulator address and positive timeout are required")
	}
	pem, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read simulator CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, nil, fmt.Errorf("simulator CA contains no certificates")
	}
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load governance client certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, nil, err
	}
	trusted := false
	for _, name := range leaf.DNSNames {
		if name == "ani-governance" {
			trusted = true
		}
	}
	if !trusted {
		return nil, nil, fmt.Errorf("governance certificate requires exact DNS SAN ani-governance")
	}
	conn, err := grpc.NewClient(cfg.Address,
		grpc.WithDisableServiceConfig(),
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			MinVersion:   tls.VersionTLS13,
			RootCAs:      roots,
			Certificates: []tls.Certificate{cert},
			ServerName:   "ani-gpu-simulator",
		})))
	if err != nil {
		return nil, nil, err
	}
	return &GpuSimulatorAdapter{
		client:  quotalabpb.NewGpuSimulatorServiceClient(conn),
		timeout: cfg.Timeout,
	}, func() { _ = conn.Close() }, nil
}

// GpuSimulatorAdapter 实现 QuotaDispatchAdapter 与 GpuResourceReader。
type GpuSimulatorAdapter struct {
	client  quotalabpb.GpuSimulatorServiceClient
	timeout time.Duration
}

func (a *GpuSimulatorAdapter) OwnerService() string { return data.QuotaCodeOwnerLab }

func (a *GpuSimulatorAdapter) Actions() []string { return []string{"LAB_GPU_CREATE", "LAB_GPU_DELETE"} }

// Dispatch 固定 RPC 超时 3 秒；区分持久接受（ACK nil err）、永久合同错误、
// 传输不确定。ACK 只用于结束投递重试，不代表创建成功（§11.3）。
func (a *GpuSimulatorAdapter) Dispatch(ctx context.Context, cmd *service.QuotaDispatchCommand) ([]byte, error) {
	if len(cmd.Charges) == 0 {
		return nil, fmt.Errorf("command carries no charge")
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	switch cmd.Action {
	case "LAB_GPU_CREATE":
		if len(cmd.Charges) != 1 {
			return nil, fmt.Errorf("LAB_GPU_CREATE expects exactly one charge")
		}
		if cmd.Charges[0].QuotaCode != data.QuotaCodeGpuCount {
			return nil, fmt.Errorf("quota_code must be gpu.count")
		}
		// AcceptCreate 要求 charged_units 等于 gpu_count（§11.3）；
		// gpu_count 由占额校验后的业务参数还原。
		gpuCount := int32(cmd.Charges[0].ChargedUnits)
		name := extractCanonicalField(string(cmd.CanonicalRequest), "name")
		if name == "" {
			return nil, fmt.Errorf("canonical request is missing name")
		}
		reply, err := a.client.AcceptCreate(ctx, &quotalabpb.AcceptCreateRequest{
			OperationId:     cmd.OperationID,
			ResourceId:      cmd.ResourceID,
			TenantId:        cmd.ResourceTenantID,
			Actor:           cmd.Actor,
			RequestHash:     cmd.RequestHash,
			Name:            name,
			GpuCount:        gpuCount,
			ChargeId:        cmd.Charges[0].ChargeID,
			QuotaCode:       cmd.Charges[0].QuotaCode,
			ChargedUnits:    cmd.Charges[0].ChargedUnits,
		})
		if err != nil {
			return nil, err
		}
		return []byte(fmt.Sprintf(`{"operation_id":%q,"resource_id":%q,"accepted":%t}`,
			reply.GetOperationId(), reply.GetResourceId(), reply.GetAccepted())), nil
	case "LAB_GPU_DELETE":
		if cmd.CreateOperationID == "" {
			return nil, fmt.Errorf("LAB_GPU_DELETE requires create_operation_id")
		}
		reply, err := a.client.AcceptDelete(ctx, &quotalabpb.AcceptDeleteRequest{
			OperationId:      cmd.OperationID,
			CreateOperationId: cmd.CreateOperationID,
			ResourceId:       cmd.ResourceID,
			TenantId:         cmd.ResourceTenantID,
			Actor:            cmd.Actor,
			RequestHash:      cmd.RequestHash,
			ChargeId:         cmd.Charges[0].ChargeID,
		})
		if err != nil {
			return nil, err
		}
		return []byte(fmt.Sprintf(`{"operation_id":%q,"resource_id":%q,"accepted":%t}`,
			reply.GetOperationId(), reply.GetResourceId(), reply.GetAccepted())), nil
	default:
		return nil, fmt.Errorf("unsupported action %q", cmd.Action)
	}
}

// GetResource 纯读取转发；不推进创建/清理。
func (a *GpuSimulatorAdapter) GetResource(ctx context.Context, tenantID, resourceID string) (*quotalabpb.GetResourceResponse, error) {
	if _, err := uuid.Parse(tenantID); err != nil {
		return nil, fmt.Errorf("invalid tenant id")
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	return a.client.GetResource(ctx, &quotalabpb.GetResourceRequest{TenantId: tenantID, ResourceId: resourceID})
}

// extractCanonicalField 从规范请求 JSON 中提取固定字段（规范格式受控，
// 不解析任意用户 JSON）。
func extractCanonicalField(canonical, field string) string {
	needle := `"` + field + `":`
	idx := indexAfter(canonical, needle)
	if idx < 0 {
		return ""
	}
	rest := canonical[idx:]
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:]
	end := 0
	for end < len(rest) {
		if rest[end] == '\\' {
			end += 2
			continue
		}
		if rest[end] == '"' {
			break
		}
		end++
	}
	if end > len(rest) {
		return ""
	}
	return unescapeJSON(rest[:end])
}

func indexAfter(s, needle string) int {
	for i := 0; i+len(needle) <= len(s); i++ {
		if s[i:i+len(needle)] == needle {
			return i + len(needle)
		}
	}
	return -1
}

func unescapeJSON(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				out = append(out, '\n')
			case 't':
				out = append(out, '\t')
			default:
				out = append(out, s[i])
			}
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}
