package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2"

	"alexagent/internal/netguard"
)

// webTools fetch from the web directly. web_search is the model's hosted
// tool, added in New.
func webTools() []*tool {
	return []*tool{
		{
			Name:        "fetch_url",
			Description: "Fetch a URL directly over HTTP (no JavaScript, no cookies) and return its content — HTML as markdown, JSON/text as-is. Good for APIs, feeds and simple pages. Set save_as to download the file into the workspace instead.",
			Params:      map[string]any{"url": p("string", "URL"), "save_as": p("string", "Optional workspace path to save the response body to")},
			Required:    []string{"url"},
			Run:         fetchURL,
		},
	}
}

// webGuard keeps fetch_url on the public internet (BH-4): a URL can come
// from a page Alexa read, so it mustn't reach the server's own network
// (loopback, the LAN, a cloud's metadata service). Every connection,
// redirects included, is dialled through it. Tests let it reach their
// local servers.
var webGuard = &netguard.Dialer{Connect: (&net.Dialer{Timeout: 10 * time.Second}).DialContext}

// webClient is fetch_url's HTTP client.
var webClient = &http.Client{
	Transport: &http.Transport{
		Proxy: nil, // a proxy would make the address check meaningless
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return webGuard.DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  30 * time.Second,
		MaxResponseHeaderBytes: 1 << 20,
		IdleConnTimeout:        30 * time.Second,
		ForceAttemptHTTP2:      true,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("stopped after 5 redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return fmt.Errorf("redirected to a %s: address", req.URL.Scheme)
		}
		return nil
	},
}

const (
	fetchTimeout = 60 * time.Second // the whole request, the body included
	fetchMax     = 5 << 20          // what's read to show
	fetchSaveMax = 200 << 20        // what save_as writes
)

func fetchURL(tc *toolCtx, a map[string]any) (*toolResult, error) {
	u := str(a, "url")
	ctx, cancel := context.WithTimeout(tc.ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/json,text/plain,*/*")
	resp, err := webClient.Do(req)
	if errors.Is(err, netguard.ErrBlocked) {
		return nil, fmt.Errorf("fetch_url only reaches public internet addresses, and %s (or a page it redirected to) is a local or private one", req.URL.Hostname())
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if dest := str(a, "save_as"); dest != "" {
		// Written through the workspace root, so a link swapped in for the
		// path can't send the download outside it.
		f, err := tc.run.env.Create(dest, os.O_TRUNC)
		if err != nil {
			return nil, err
		}
		n, err := io.Copy(f, io.LimitReader(resp.Body, fetchSaveMax))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, err
		}
		cut := ""
		if n == fetchSaveMax {
			cut = fmt.Sprintf(" It was cut off at %d MB.", fetchSaveMax>>20)
		}
		return &toolResult{Text: fmt.Sprintf("HTTP %d. Saved %d bytes to %s (%s).%s", resp.StatusCode, n, tc.run.env.Clean(dest), resp.Header.Get("Content-Type"), cut)}, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchMax))
	if err != nil {
		return nil, err
	}
	ct := resp.Header.Get("Content-Type")
	text := string(body)
	if strings.Contains(ct, "html") {
		if md, err := htmltomd.ConvertString(text); err == nil {
			text = md
		}
	} else if !strings.HasPrefix(ct, "text/") && !strings.Contains(ct, "json") && !strings.Contains(ct, "xml") && !strings.Contains(ct, "javascript") && ct != "" {
		return &toolResult{Text: fmt.Sprintf("HTTP %d, %s, %d bytes of binary content. Use save_as to download it.", resp.StatusCode, ct, len(body))}, nil
	}
	if len(text) > readPage {
		text = cutText(text, readPage) + "\n[…truncated]"
	}
	return &toolResult{Text: fmt.Sprintf("HTTP %d (%s)\n\n%s", resp.StatusCode, ct, text), Kind: "long"}, nil
}
