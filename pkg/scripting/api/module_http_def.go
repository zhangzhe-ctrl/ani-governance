package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// ModuleHTTP 构建语言无关的 http 模块（JS 等基于 map[string]any 桥接的语言使用）。
// 与 Lua 版 LoaderHTTP 共用 httpRequest/buildHTTPClient/checkAllowedURL 护栏：
// 白名单 fail-closed、重定向逐跳复检、超时/体积上限。
// 返回的函数 (T, error) 形态在 goja 桥接下转为 JS 异常。
func ModuleHTTP(opts HTTPOptions) ModuleDef {
	normalized := normalizeHTTPOptions(opts)
	client := buildHTTPClient(normalized)

	blocked := func(rawURL string) error {
		if err := checkAllowedURL(rawURL, normalized.AllowedDomains, normalized.DenyLoopback); err != nil {
			return errInvalid("http blocked: " + err.Error())
		}
		return nil
	}

	return ModuleDef{
		Name: "http",
		Funcs: map[string]any{
			// request(url, options?) → {status, body, headers}
			"request": func(rawURL string, options ...map[string]any) (map[string]any, error) {
				if err := blocked(rawURL); err != nil {
					return nil, err
				}
				method := "GET"
				headers := map[string]string{}
				body := ""
				if len(options) > 0 && options[0] != nil {
					o := options[0]
					if m, ok := o["method"].(string); ok && m != "" {
						method = strings.ToUpper(m)
					}
					if h, ok := o["headers"].(map[string]any); ok {
						for k, v := range h {
							headers[k] = toStringValue(v)
						}
					}
					if b, ok := o["body"].(string); ok {
						body = b
					}
				}
				return httpRequest(context.Background(), client, rawURL, method, headers, body, normalized.MaxResponseBody)
			},
			// get(url) → {status, body, headers}
			"get": func(rawURL string) (map[string]any, error) {
				if err := blocked(rawURL); err != nil {
					return nil, err
				}
				return httpRequest(context.Background(), client, rawURL, http.MethodGet, nil, "", normalized.MaxResponseBody)
			},
			// post(url, body, contentType?) → {status, body, headers}
			"post": func(rawURL, body string, contentType ...string) (map[string]any, error) {
				if err := blocked(rawURL); err != nil {
					return nil, err
				}
				ct := "application/json"
				if len(contentType) > 0 && contentType[0] != "" {
					ct = contentType[0]
				}
				return httpRequest(context.Background(), client, rawURL, http.MethodPost,
					map[string]string{"Content-Type": ct}, body, normalized.MaxResponseBody)
			},
		},
	}
}

func toStringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
