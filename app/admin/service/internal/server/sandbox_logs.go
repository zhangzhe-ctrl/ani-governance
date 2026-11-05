package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	res "github.com/zhangzhe-ctrl/ani-resource-service/api/sandbox/v1"
	view "go-wind-admin/api/gen/go/sandbox/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type sandboxLogHTTP struct {
	service *service.SandboxService
	guard   *instanceLogHTTP
}

func RegisterSandboxLogsHTTP(server *khttp.Server, s *service.SandboxService, configured ...InstanceLogLimits) {
	limits := DefaultInstanceLogLimits()
	if len(configured) > 1 {
		panic("multiple sandbox log configurations")
	}
	if len(configured) == 1 {
		limits = configured[0]
	}
	if err := limits.validate(); err != nil {
		panic(err)
	}
	guard := &instanceLogHTTP{limits: limits, tenants: map[string]int{}, instances: map[string]int{}}
	server.Route("/").GET(data.SandboxLogsPath, (&sandboxLogHTTP{service: s, guard: guard}).handle)
}

func decodeSandboxLogQuery(r *http.Request) (*view.StreamSandboxLogsRequest, error) {
	invalid := kratoserrors.BadRequest("INVALID_SANDBOX_REQUEST", "invalid log options")
	if r == nil || r.URL == nil || r.URL.ForceQuery || r.URL.RawPath != "" && r.URL.RawPath != r.URL.Path || len(r.URL.RawQuery) > 2048 || r.ContentLength > 0 || len(r.TransferEncoding) != 0 {
		return nil, invalid
	}
	if r.Body != nil {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(raw) != 0 {
			return nil, invalid
		}
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, invalid
	}
	out := new(view.StreamSandboxLogsRequest)
	for key, entries := range values {
		if len(entries) != 1 {
			return nil, invalid
		}
		value := entries[0]
		switch key {
		case "tailLines":
			n, err := strconv.ParseUint(value, 10, 32)
			if err != nil || n < 1 || n > 1000 || strconv.FormatUint(n, 10) != value {
				return nil, invalid
			}
			tail := uint32(n)
			out.TailLines = &tail
		case "follow":
			if value != "true" && value != "false" {
				return nil, invalid
			}
			follow := value == "true"
			out.Follow = &follow
		case "sinceTime":
			if value == "" {
				return nil, invalid
			}
			out.SinceTime = value
		case "podUid":
			if value == "" {
				return nil, invalid
			}
			out.PodUid = value
		default:
			return nil, invalid
		}
	}
	if (out.SinceTime == "") != (out.PodUid == "") || out.SinceTime != "" && out.TailLines != nil {
		return nil, invalid
	}
	return out, nil
}

func (h *sandboxLogHTTP) handle(ctx khttp.Context) error {
	khttp.SetOperation(ctx, "/admin.service.v1.SandboxService/StreamSandboxLogs")
	r, err := decodeSandboxLogQuery(ctx.Request())
	if err != nil {
		return err
	}
	path := new(view.StreamSandboxLogsRequest)
	if err = ctx.BindVars(path); err != nil {
		return err
	}
	r.SandboxId = path.SandboxId
	_, err = ctx.Middleware(func(call context.Context, in any) (any, error) {
		return nil, h.stream(call, ctx.Response(), in.(*view.StreamSandboxLogsRequest))
	})(ctx, r)
	if err != nil {
		if status.Code(err) == codes.ResourceExhausted {
			return kratoserrors.New(429, "SANDBOX_LOG_CONNECTION_LIMIT", "sandbox log connection limit reached")
		}
		return sandboxHTTPError(err)
	}
	return nil
}

type sandboxLogRead struct {
	frame *res.StreamSandboxLogsResponse
	err   error
}

func validSandboxLogUID(uid string) bool {
	return uid != "" && len(uid) <= 128 && utf8.ValidString(uid) && !strings.ContainsFunc(uid, unicode.IsControl)
}

func (h *sandboxLogHTTP) stream(parent context.Context, w http.ResponseWriter, r *view.StreamSandboxLogsRequest) error {
	releaseOpening, err := h.guard.reserveOpen()
	if err != nil {
		return err
	}
	defer releaseOpening()
	limits := h.guard.limits
	call, cancel := context.WithTimeout(parent, limits.Lifetime)
	defer cancel()
	var expired atomic.Bool
	opening := time.AfterFunc(limits.OpenTimeout, func() { expired.Store(true); cancel() })
	defer opening.Stop()
	session, err := h.service.OpenSandboxLogs(call, r)
	if err != nil {
		if expired.Load() {
			return status.Error(codes.Unavailable, "SANDBOX_LOG_OPEN_TIMEOUT")
		}
		return err
	}
	release, err := h.guard.acquire(session.TenantID, session.SandboxID)
	releaseOpening()
	if err != nil {
		return err
	}
	defer release()
	// Account for the serialized metadata as well as Data. Keep room for
	// the frames and encoded text concurrently held outside the queue.
	capacity := (limits.MaxBufferedBytes - 6*limits.MaxFrameBytes) / (limits.MaxFrameBytes + 256)
	if capacity < 0 {
		capacity = 0
	}
	if capacity > 8 {
		capacity = 8
	}
	reads := make(chan sandboxLogRead, capacity)
	go func() {
		for {
			frame, err := session.Stream.Recv()
			if err == nil && (frame == nil || len(frame.Data) > limits.MaxFrameBytes || len(frame.Kind) > 8 || len(frame.Reason) > 32 || len(frame.PodUid) > 128 ||
				proto.Size(frame) > limits.MaxFrameBytes+256 || len(frame.ProtoReflect().GetUnknown()) != 0) {
				// Do not retain an invalid (potentially 20 KiB) message in
				// the bounded queue.
				frame, err = nil, status.Error(codes.Internal, "SANDBOX_LOG_UPSTREAM_PROTOCOL")
			}
			select {
			case reads <- sandboxLogRead{frame, err}:
			case <-call.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	var podUID string
	select {
	case first := <-reads:
		if expired.Load() {
			return status.Error(codes.Unavailable, "SANDBOX_LOG_OPEN_TIMEOUT")
		}
		if first.err != nil {
			if errors.Is(first.err, io.EOF) {
				return status.Error(codes.Unavailable, "SANDBOX_LOG_UPSTREAM_PROTOCOL")
			}
			return first.err
		}
		if first.frame.Kind != "READY" || len(first.frame.Data) != 0 || first.frame.Reason != "" || !validSandboxLogUID(first.frame.PodUid) || r.PodUid != "" && r.PodUid != first.frame.PodUid {
			return status.Error(codes.Unavailable, "SANDBOX_LOG_UPSTREAM_PROTOCOL")
		}
		podUID = first.frame.PodUid
	case <-call.Done():
		if expired.Load() {
			return status.Error(codes.Unavailable, "SANDBOX_LOG_OPEN_TIMEOUT")
		}
		return call.Err()
	}
	opening.Stop()
	if expired.Load() {
		return status.Error(codes.Unavailable, "SANDBOX_LOG_OPEN_TIMEOUT")
	}
	if err = call.Err(); err != nil {
		return err
	}
	controller := http.NewResponseController(w)
	if err = controller.SetWriteDeadline(time.Now().Add(limits.WriteTimeout)); err != nil {
		return status.Error(codes.Unavailable, "SANDBOX_LOG_TRANSPORT_UNAVAILABLE")
	}
	defer controller.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	writer := instanceSSEWriter{w: w, controller: controller, timeout: limits.WriteTimeout, textChunk: (limits.MaxFrameBytes - 32) / 6}
	if writer.event("ready", struct {
		PodUID string `json:"pod_uid"`
	}{podUID}) != nil {
		return nil
	}
	ticker := time.NewTicker(limits.Heartbeat)
	defer ticker.Stop()
	decoder := instanceLogUTF8{}
	cursor := sandboxLogCursor{}
	if r.SinceTime != "" {
		// OpenSandboxLogs has already validated this value. It may be a
		// position remembered by the client from a previous connection.
		cursor.lastEmitted, _ = time.Parse(time.RFC3339Nano, r.SinceTime)
		cursor.lastEmitted = cursor.lastEmitted.UTC()
	}
	writeLog := func(text string) error {
		if text == "" {
			return nil
		}
		// Publish a cursor only after every corresponding text fragment has
		// been written. On failure the previous cursor remains replayable.
		if err := writer.text(text); err != nil {
			return err
		}
		since := cursor.advance(text)
		if cursor.takeInvalidation() {
			return writer.event("cursor_invalidated", struct {
				PodUID string `json:"pod_uid"`
				Reason string `json:"reason"`
			}{podUID, "TIMESTAMP_UNAVAILABLE"})
		}
		if since != "" {
			return writer.event("cursor", struct {
				PodUID    string `json:"pod_uid"`
				SinceTime string `json:"since_time"`
			}{podUID, since})
		}
		return nil
	}
	finish := func(event, reason string) {
		if text := decoder.decode(nil, true); text != "" {
			if writeLog(text) != nil {
				return
			}
		}
		_ = writer.event(event, struct {
			Reason string `json:"reason"`
		}{reason})
	}
	for {
		select {
		case result := <-reads:
			if result.err != nil {
				if errors.Is(call.Err(), context.DeadlineExceeded) && parent.Err() == nil {
					finish("end", "TIME_LIMIT")
				} else if errors.Is(result.err, io.EOF) {
					finish("error", "SANDBOX_LOG_UPSTREAM_PROTOCOL")
				} else if status.Code(result.err) == codes.Internal && status.Convert(result.err).Message() == "SANDBOX_LOG_UPSTREAM_PROTOCOL" {
					finish("error", "SANDBOX_LOG_UPSTREAM_PROTOCOL")
				} else if call.Err() == nil {
					finish("error", "SANDBOX_LOG_UPSTREAM_UNAVAILABLE")
				}
				return nil
			}
			frame := result.frame
			switch frame.Kind {
			case "DATA":
				if frame.Reason != "" || frame.PodUid != "" {
					finish("error", "SANDBOX_LOG_UPSTREAM_PROTOCOL")
					return nil
				}
				if text := decoder.decode(frame.Data, false); text != "" {
					if writeLog(text) != nil {
						return nil
					}
				}
			case "END":
				if len(frame.Data) != 0 || frame.PodUid != "" || (frame.Reason != "EOF" && frame.Reason != "TIME_LIMIT" && frame.Reason != "BYTE_LIMIT" && frame.Reason != "TARGET_UNAVAILABLE") {
					finish("error", "SANDBOX_LOG_UPSTREAM_PROTOCOL")
				} else {
					finish("end", frame.Reason)
				}
				return nil
			case "ERROR":
				if len(frame.Data) != 0 || frame.PodUid != "" || frame.Reason != "UPSTREAM_UNAVAILABLE" {
					finish("error", "SANDBOX_LOG_UPSTREAM_PROTOCOL")
				} else {
					finish("error", "SANDBOX_LOG_UPSTREAM_UNAVAILABLE")
				}
				return nil
			default:
				finish("error", "SANDBOX_LOG_UPSTREAM_PROTOCOL")
				return nil
			}
		case <-ticker.C:
			if writer.heartbeat() != nil {
				return nil
			}
		case <-call.Done():
			if errors.Is(call.Err(), context.DeadlineExceeded) && parent.Err() == nil {
				finish("end", "TIME_LIMIT")
			}
			return nil
		}
	}
}
