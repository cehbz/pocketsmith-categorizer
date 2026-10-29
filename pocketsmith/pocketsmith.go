// Package pocketsmith is a client for the parts of PocketSmith's v2 API the categorizer uses.
package pocketsmith

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// DefaultBaseURL is PocketSmith's v2 API.
const DefaultBaseURL = "https://api.pocketsmith.com/v2"

// Client calls PocketSmith with a developer key.
type Client struct {
	BaseURL string
	Key     string
	HTTP    *http.Client
}

var nextLink = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// do sends a request and decodes the response into out, returning the next page's URL if any.
func (c *Client) do(ctx context.Context, method, url string, body, out any) (string, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return "", err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Developer-Key", c.Key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("pocketsmith %s %s: %s: %s", method, req.URL.Path, resp.Status, bytes.TrimSpace(msg))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return "", fmt.Errorf("pocketsmith %s %s: %w", method, req.URL.Path, err)
		}
	}
	if m := nextLink.FindStringSubmatch(resp.Header.Get("Link")); m != nil {
		return m[1], nil
	}
	return "", nil
}

type transaction struct {
	ID       int64   `json:"id"`
	Date     string  `json:"date"`
	Amount   float64 `json:"amount"`
	Payee    string  `json:"payee"`
	Category *struct {
		ID int64 `json:"id"`
	} `json:"category"`
	NeedsReview bool     `json:"needs_review"`
	Labels      []string `json:"labels"`
}

func (t transaction) charge() (ledger.Charge, error) {
	d, err := day.Parse(t.Date)
	if err != nil {
		return ledger.Charge{}, fmt.Errorf("transaction %d date %q: %w", t.ID, t.Date, err)
	}
	var cat ledger.CategoryID
	if t.Category != nil {
		cat = ledger.CategoryID(t.Category.ID)
	}
	return ledger.Charge{
		ID:    t.ID,
		USD:   -t.Amount,
		Date:  d,
		Payee: t.Payee,
		State: ledger.State{Category: cat, NeedsReview: t.NeedsReview, Labels: t.Labels},
	}, nil
}

// Charges lists an account's transactions from first to last whose payee is one of payees.
func (c *Client) Charges(ctx context.Context, account int64, first, last time.Time, payees []string) ([]ledger.Charge, error) {
	url := fmt.Sprintf("%s/transaction_accounts/%d/transactions?start_date=%s&end_date=%s&per_page=100",
		c.BaseURL, account, day.Format(first), day.Format(last))
	var charges []ledger.Charge
	for url != "" {
		var page []transaction
		next, err := c.do(ctx, http.MethodGet, url, nil, &page)
		if err != nil {
			return nil, err
		}
		for _, t := range page {
			if !slices.Contains(payees, t.Payee) {
				continue
			}
			ch, err := t.charge()
			if err != nil {
				return nil, err
			}
			charges = append(charges, ch)
		}
		url = next
	}
	return charges, nil
}

// Charge reads one transaction.
func (c *Client) Charge(ctx context.Context, id int64) (ledger.Charge, error) {
	var t transaction
	if _, err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s/transactions/%d", c.BaseURL, id), nil, &t); err != nil {
		return ledger.Charge{}, err
	}
	return t.charge()
}

// Update sets a transaction's category, review flag and labels; category zero uncategorizes it.
func (c *Client) Update(ctx context.Context, id int64, s ledger.State) error {
	var category any = int64(s.Category)
	if s.Category == 0 {
		category = ""
	}
	body := map[string]any{
		"category_id":  category,
		"needs_review": s.NeedsReview,
		"labels":       strings.Join(s.Labels, ","),
	}
	_, err := c.do(ctx, http.MethodPut, fmt.Sprintf("%s/transactions/%d", c.BaseURL, id), body, nil)
	return err
}

// TimeZone returns the location of the user's PocketSmith time zone.
func (c *Client) TimeZone(ctx context.Context) (*time.Location, error) {
	var me struct {
		TimeZone string `json:"time_zone"`
	}
	if _, err := c.do(ctx, http.MethodGet, c.BaseURL+"/me", nil, &me); err != nil {
		return nil, err
	}
	var zones []struct {
		Name       string `json:"name"`
		Identifier string `json:"identifier"`
	}
	if _, err := c.do(ctx, http.MethodGet, c.BaseURL+"/time_zones", nil, &zones); err != nil {
		return nil, err
	}
	for _, z := range zones {
		if z.Name == me.TimeZone {
			return time.LoadLocation(z.Identifier)
		}
	}
	return nil, fmt.Errorf("PocketSmith time zone %q has no identifier", me.TimeZone)
}
