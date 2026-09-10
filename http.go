package typetown

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// Default limits for the HTTP handler.
const (
	defaultLimit = 10
	maxLimit     = 50
)

// handler serves place lookups. It is created by Handler.
type handler struct {
	ix           *Index
	defaultLimit int
	maxLimit     int
	home         string
	homeHeader   string
	cors         []string
	cacheControl string
}

// HandlerOption configures the handler returned by Handler.
type HandlerOption func(*handler)

// WithLimit sets the default and maximum number of results. A request may ask
// for fewer than the maximum but never more, so a caller cannot turn one request
// into an expensive one.
func WithLimit(def, max int) HandlerOption {
	return func(h *handler) {
		if def > 0 {
			h.defaultLimit = def
		}
		if max > 0 {
			h.maxLimit = max
		}
	}
}

// WithHome sets the country results are biased toward when a request does not
// name one.
func WithHome(cc string) HandlerOption {
	return func(h *handler) { h.home = strings.ToUpper(cc) }
}

// WithHomeHeader reads the caller's country from a request header when the
// request does not name one — "CF-IPCountry" behind Cloudflare, "X-Country-Code"
// behind many other proxies. An explicit home parameter still wins.
//
// Only set this when a trusted proxy sets the header, since a client can
// otherwise send whatever it likes. The consequence is mild — results are
// ordered differently — but it is still input from the network.
func WithHomeHeader(name string) HandlerOption {
	return func(h *handler) { h.homeHeader = http.CanonicalHeaderKey(name) }
}

// WithCORS allows cross-origin requests from the given origins, or from any
// origin if passed "*". Without it no CORS headers are sent, which is what you
// want when the endpoint is called from your own backend.
func WithCORS(origins ...string) HandlerOption {
	return func(h *handler) { h.cors = origins }
}

// WithCacheControl sets the Cache-Control header on successful responses. An
// index is immutable once built, so caching answers is safe until it is
// replaced; the default allows five minutes.
func WithCacheControl(v string) HandlerOption {
	return func(h *handler) { h.cacheControl = v }
}

// Handler returns an http.Handler that answers place lookups as JSON.
//
// It handles one route and does not care where it is mounted:
//
//	GET <mount>?q=lond&limit=5&home=GB
//	GET <mount>?q=lond&limit=5&lat=42.33&lon=-83.05
//
//	{"query":"lond","results":[{"id":2643743,"name":"London", ... }]}
//
// A missing or empty q is 400, a non-GET method is 405, and a query that
// matches nothing is 200 with an empty list — finding no such town is an
// answer, not an error.
func Handler(ix *Index, opts ...HandlerOption) http.Handler {
	h := &handler{
		ix:           ix,
		defaultLimit: defaultLimit,
		maxLimit:     maxLimit,
		cacheControl: "public, max-age=300",
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

type response struct {
	Query   string   `json:"query"`
	Results []Result `json:"results"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.setCORS(w, r)
	if r.Method == http.MethodOptions && len(h.cors) > 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		h.fail(w, http.StatusMethodNotAllowed, "use GET")
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		h.fail(w, http.StatusBadRequest, "missing query parameter q")
		return
	}

	limit := h.defaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			h.fail(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = min(n, h.maxLimit)
	}

	near, err := nearFrom(r)
	if err != nil {
		h.fail(w, http.StatusBadRequest, err.Error())
		return
	}

	results, err := h.ix.Search(q, SearchOptions{
		Limit: limit, Home: h.homeFor(r), Near: near,
	})
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "search failed")
		return
	}
	if results == nil {
		results = []Result{} // an empty list, not a JSON null
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if h.cacheControl != "" {
		w.Header().Set("Cache-Control", h.cacheControl)
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(w).Encode(response{Query: q, Results: results})
}

// nearFrom reads the caller's position from the query string. lat and lon come
// as a pair: one without the other is a mistake worth reporting rather than
// silently ignoring. A position does not replace the country hint — both are
// passed, and the ranking takes whichever helps more.
func nearFrom(r *http.Request) (*LatLon, error) {
	q := r.URL.Query()
	latRaw := strings.TrimSpace(q.Get("lat"))
	lonRaw := strings.TrimSpace(q.Get("lon"))
	if latRaw == "" && lonRaw == "" {
		return nil, nil
	}
	if latRaw == "" || lonRaw == "" {
		return nil, errors.New("lat and lon must be given together")
	}
	lat, err := strconv.ParseFloat(latRaw, 64)
	if err != nil || lat < -90 || lat > 90 {
		return nil, errors.New("lat must be a number between -90 and 90")
	}
	lon, err := strconv.ParseFloat(lonRaw, 64)
	if err != nil || lon < -180 || lon > 180 {
		return nil, errors.New("lon must be a number between -180 and 180")
	}
	return &LatLon{Lat: lat, Lon: lon}, nil
}

// homeFor resolves the country to bias toward: the request's own parameter
// first, then a trusted header, then the configured default.
func (h *handler) homeFor(r *http.Request) string {
	if v := strings.TrimSpace(r.URL.Query().Get("home")); v != "" {
		return strings.ToUpper(v)
	}
	if h.homeHeader != "" {
		if v := strings.TrimSpace(r.Header.Get(h.homeHeader)); v != "" && v != "XX" {
			return strings.ToUpper(v)
		}
	}
	return h.home
}

func (h *handler) setCORS(w http.ResponseWriter, r *http.Request) {
	if len(h.cors) == 0 {
		return
	}
	origin := r.Header.Get("Origin")
	allowed := ""
	for _, o := range h.cors {
		if o == "*" {
			allowed = "*"
			break
		}
		if o == origin {
			allowed = origin
			break
		}
	}
	if allowed == "" {
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", allowed)
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	if allowed != "*" {
		w.Header().Add("Vary", "Origin")
	}
}

func (h *handler) fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(errorResponse{Error: msg})
}
