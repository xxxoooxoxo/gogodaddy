package godaddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	BaseURL    string
	APIKey     string
	APISecret  string
	ShopperID  string
	HTTPClient *http.Client
}

type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Body       string
}

func (e *APIError) Error() string {
	if e.Code != "" && e.Message != "" {
		return fmt.Sprintf("godaddy api returned %d %s: %s", e.StatusCode, e.Code, e.Message)
	}
	if e.Message != "" {
		return fmt.Sprintf("godaddy api returned %d: %s", e.StatusCode, e.Message)
	}
	if e.Body != "" {
		return fmt.Sprintf("godaddy api returned %d: %s", e.StatusCode, e.Body)
	}
	return fmt.Sprintf("godaddy api returned %d", e.StatusCode)
}

func NewClient(baseURL, apiKey, apiSecret, shopperID string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
		APISecret:  apiSecret,
		ShopperID:  shopperID,
		HTTPClient: httpClient,
	}
}

func (c *Client) AddRecords(ctx context.Context, domain string, records []DNSRecord) error {
	for _, record := range records {
		if err := ValidateRecord(record, true); err != nil {
			return err
		}
	}
	return c.do(ctx, http.MethodPatch, "/v1/domains/"+url.PathEscape(domain)+"/records", nil, records, nil)
}

func (c *Client) GetRecords(ctx context.Context, domain, recordType, name string, offset, limit int) ([]DNSRecord, error) {
	if err := ValidateTypeName(recordType, name); err != nil {
		return nil, err
	}

	query := url.Values{}
	if offset > 0 {
		query.Set("offset", fmt.Sprint(offset))
	}
	if limit > 0 {
		query.Set("limit", fmt.Sprint(limit))
	}

	var records []DNSRecord
	err := c.do(ctx, http.MethodGet, recordTypeNamePath(domain, recordType, name), query, nil, &records)
	return records, err
}

func (c *Client) ReplaceRecordsByTypeName(ctx context.Context, domain, recordType, name string, records []DNSRecord) error {
	if err := ValidateTypeName(recordType, name); err != nil {
		return err
	}

	body := make([]DNSRecord, len(records))
	for i, record := range records {
		record.Type = ""
		record.Name = ""
		if err := ValidateRecord(record, false); err != nil {
			return err
		}
		body[i] = record
	}
	return c.do(ctx, http.MethodPut, recordTypeNamePath(domain, recordType, name), nil, body, nil)
}

func (c *Client) DeleteRecordsByTypeName(ctx context.Context, domain, recordType, name string) error {
	if err := ValidateTypeName(recordType, name); err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, recordTypeNamePath(domain, recordType, name), nil, nil, nil)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	if c.APIKey == "" || c.APISecret == "" {
		return fmt.Errorf("missing GoDaddy API credentials; run `gogodaddy auth login` or set GODADDY_API_KEY and GODADDY_API_SECRET")
	}
	if c.BaseURL == "" {
		return fmt.Errorf("missing GoDaddy API base URL")
	}

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	endpoint := c.BaseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "sso-key "+c.APIKey+":"+c.APISecret)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "gogodaddy/0.1")
	if c.ShopperID != "" {
		req.Header.Set("X-Shopper-Id", c.ShopperID)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return decodeAPIError(resp.StatusCode, respBody)
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func decodeAPIError(status int, body []byte) error {
	apiErr := &APIError{StatusCode: status, Body: strings.TrimSpace(string(body))}
	var decoded struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &decoded); err == nil {
		apiErr.Code = decoded.Code
		apiErr.Message = decoded.Message
	}
	return apiErr
}

func recordTypeNamePath(domain, recordType, name string) string {
	return "/v1/domains/" + url.PathEscape(domain) + "/records/" + url.PathEscape(recordType) + "/" + url.PathEscape(name)
}
