package mcpserver

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewHTTPHandler exposes /mcp with a dedicated token. The token authenticates
// access to this adapter and is never forwarded as a gateway credential.
func NewHTTPHandler(server *mcp.Server, token string) (http.Handler, error) {
	if strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n") {
		return nil, fmt.Errorf("a nonempty MCP HTTP token without newlines is required")
	}
	expected := sha256.Sum256([]byte("Bearer " + token))
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 4 << 20,
		PropagateRequestCancellation: true,
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(provided[:], expected[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="ctrail-mcp"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
				!strings.EqualFold(u.Host, r.Host) || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
				http.Error(w, "Origin not allowed", http.StatusForbidden)
				return
			}
		}
		handler.ServeHTTP(w, r)
	}), nil
}
