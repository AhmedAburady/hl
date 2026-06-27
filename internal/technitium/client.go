package technitium

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// RecordType is the DNS record type to add. Only A and CNAME are supported by
// this CLI, matching the homelab scope.
type RecordType string

const (
	TypeA     RecordType = "A"
	TypeCNAME RecordType = "CNAME"
)

// AddRecordRequest describes a record to add to an authoritative zone.
type AddRecordRequest struct {
	Domain    string     // FQDN of the record, e.g. app.home.lab
	Zone      string     // authoritative zone, e.g. home.lab
	Type      RecordType // A or CNAME
	Value     string     // IP for A, target for CNAME
	TTL       int        // seconds; 0 = server default
	Overwrite bool       // replace existing record set for this type
	Comments  string
}

// Client talks to the Technitium DNS Server HTTP API.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New returns a client for the Technitium server at baseURL authenticated with
// token (created via CreateToken / dns login).
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// apiResponse is the common Technitium envelope.
type apiResponse struct {
	Status       string          `json:"status"`
	ErrorMessage string          `json:"errorMessage"`
	ErrorCode    int             `json:"errorCode"`
	Response     json.RawMessage `json:"response"`
}

// APIError is returned when the server responds with a non-"ok" status.
type APIError struct {
	Status       string
	ErrorMessage string
	ErrorCode    int
}

func (e *APIError) Error() string {
	if e.ErrorMessage != "" {
		return fmt.Sprintf("technitium api %s: %s", e.Status, e.ErrorMessage)
	}
	return fmt.Sprintf("technitium api %s", e.Status)
}

// do performs a GET against the API path with the given query params, using the
// session token, and decodes the envelope.
func (c *Client) do(ctx context.Context, path string, params url.Values) (*apiResponse, error) {
	if params == nil {
		params = url.Values{}
	}
	if c.token != "" {
		params.Set("token", c.token)
	}
	u := c.baseURL + path + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d for %s: %s", resp.StatusCode, path, strings.TrimSpace(string(body)))
	}

	var ar apiResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, strings.TrimSpace(string(body)))
	}
	if ar.Status != "ok" {
		return &ar, &APIError{Status: ar.Status, ErrorMessage: ar.ErrorMessage, ErrorCode: ar.ErrorCode}
	}
	return &ar, nil
}

// CreateToken creates a persistent API token for the given user/password. If
// the account has 2FA enabled, totp must be a current authenticator code;
// otherwise it may be empty. The caller should persist the returned token via
// config.SetToken.
func (c *Client) CreateToken(ctx context.Context, user, pass, totp, name string) (string, error) {
	params := url.Values{}
	params.Set("user", user)
	params.Set("pass", pass)
	if totp != "" {
		params.Set("totp", totp)
	}
	params.Set("tokenName", name)
	params.Set("includeInfo", "false")

	ar, err := c.do(ctx, "/api/user/createToken", params)
	if err != nil {
		return "", err
	}

	// createToken returns { token: "...", ... } inside response.
	var info struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(ar.Response, &info); err != nil {
		return "", fmt.Errorf("decode createToken response: %w", err)
	}
	if info.Token == "" {
		return "", errors.New("createToken returned empty token")
	}
	return info.Token, nil
}

// AddRecord adds (or, with Overwrite, replaces) a record in the zone.
func (c *Client) AddRecord(ctx context.Context, r AddRecordRequest) error {
	if r.Domain == "" {
		return errors.New("domain is required")
	}
	if r.Type != TypeA && r.Type != TypeCNAME {
		return fmt.Errorf("unsupported record type %q (want A or CNAME)", r.Type)
	}
	if r.Value == "" {
		return errors.New("value is required")
	}

	params := url.Values{}
	params.Set("domain", r.Domain)
	if r.Zone != "" {
		params.Set("zone", r.Zone)
	}
	params.Set("type", string(r.Type))
	switch r.Type {
	case TypeA:
		params.Set("ipAddress", r.Value)
	case TypeCNAME:
		params.Set("cname", r.Value)
	}
	if r.TTL > 0 {
		params.Set("ttl", strconv.Itoa(r.TTL))
	}
	if r.Overwrite {
		params.Set("overwrite", "true")
	}
	if r.Comments != "" {
		params.Set("comments", r.Comments)
	}

	if _, err := c.do(ctx, "/api/zones/records/add", params); err != nil {
		return err
	}
	return nil
}

// DeleteRecordRequest describes a record to remove from an authoritative zone.
type DeleteRecordRequest struct {
	Domain string     // FQDN of the record, e.g. app.home.lab
	Zone   string     // authoritative zone, e.g. home.lab
	Type   RecordType // A or CNAME
	Value  string     // IP for A, target for CNAME (identifies which record)
}

// DeleteRecord removes a single record from the zone. For A and CNAME records
// the value is required to identify the specific record within the record set.
func (c *Client) DeleteRecord(ctx context.Context, r DeleteRecordRequest) error {
	if r.Domain == "" {
		return errors.New("domain is required")
	}
	if r.Type != TypeA && r.Type != TypeCNAME {
		return fmt.Errorf("unsupported record type %q (want A or CNAME)", r.Type)
	}
	if r.Value == "" {
		return errors.New("value is required")
	}

	params := url.Values{}
	params.Set("domain", r.Domain)
	if r.Zone != "" {
		params.Set("zone", r.Zone)
	}
	params.Set("type", string(r.Type))
	params.Set("value", r.Value)
	switch r.Type {
	case TypeA:
		params.Set("ipAddress", r.Value)
	case TypeCNAME:
		params.Set("cname", r.Value)
	}

	if _, err := c.do(ctx, "/api/zones/records/delete", params); err != nil {
		return err
	}
	return nil
}

// Record is a minimal view of a zone record returned by ListRecords.
type Record struct {
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	TTL      int            `json:"ttl"`
	RData    map[string]any `json:"rData"`
	Comments string         `json:"comments"`
	Zone     string         `json:"-"` // set by ListRecords from the queried zone
}

// Value returns the record's primary value (IP for A, target for CNAME),
// extracted from RData. Empty if no recognized value key is present.
func (r Record) Value() string {
	if r.RData == nil {
		return ""
	}
	for _, k := range []string{"ipAddress", "cname"} {
		if v, ok := r.RData[k]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

// ListRecords returns records for a zone (optionally filtered to a domain).
func (c *Client) ListRecords(ctx context.Context, zone, domain string) ([]Record, error) {
	if zone == "" {
		return nil, errors.New("zone is required")
	}
	params := url.Values{}
	params.Set("zone", zone)
	params.Set("listZone", "true")
	if domain != "" {
		params.Set("domain", domain)
	}

	ar, err := c.do(ctx, "/api/zones/records/get", params)
	if err != nil {
		return nil, err
	}

	var res struct {
		Records []Record `json:"records"`
	}
	if err := json.Unmarshal(ar.Response, &res); err != nil {
		return nil, fmt.Errorf("decode records list: %w", err)
	}
	for i := range res.Records {
		res.Records[i].Zone = zone
	}
	return res.Records, nil
}

// ListZones returns the names of all authoritative zones the token can view.
func (c *Client) ListZones(ctx context.Context) ([]string, error) {
	ar, err := c.do(ctx, "/api/zones/list", nil)
	if err != nil {
		return nil, err
	}
	var res struct {
		Zones []struct {
			Name string `json:"name"`
		} `json:"zones"`
	}
	if err := json.Unmarshal(ar.Response, &res); err != nil {
		return nil, fmt.Errorf("decode zones list: %w", err)
	}
	names := make([]string, 0, len(res.Zones))
	for _, z := range res.Zones {
		names = append(names, z.Name)
	}
	return names, nil
}
