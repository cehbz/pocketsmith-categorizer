// Package plaid reads card transactions from Plaid for the trips layer.
package plaid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

// BaseURL is Plaid's production API.
const BaseURL = "https://production.plaid.com"

const pageSize = 500

// Client calls Plaid for one linked item.
type Client struct {
	BaseURL     string
	ClientID    string
	Secret      string
	AccessToken string
	HTTP        *http.Client
}

// Account is one of the item's accounts.
type Account struct {
	ID   string `json:"account_id"`
	Mask string `json:"mask"`
	Name string `json:"name"`
}

// Transaction is one of the item's transactions; a charge is positive.
type Transaction struct {
	ID        string  `json:"transaction_id"`
	AccountID string  `json:"account_id"`
	Date      string  `json:"date"`
	Amount    float64 `json:"amount"`
	Name      string  `json:"name"`
	Merchant  string  `json:"merchant_name"`
	Code      string  `json:"merchant_category_code"`
	Channel   string  `json:"payment_channel"`
	Pending   bool    `json:"pending"`
}

// Accounts lists the item's accounts.
func (c *Client) Accounts(ctx context.Context) ([]Account, error) {
	var out struct {
		Accounts []Account `json:"accounts"`
	}
	if err := c.post(ctx, "/accounts/get", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Accounts, nil
}

// Transactions lists the item's transactions dated from to to, a page at a time.
func (c *Client) Transactions(ctx context.Context, from, to time.Time) ([]Transaction, error) {
	var all []Transaction
	for {
		var page struct {
			Transactions []Transaction `json:"transactions"`
			Total        int           `json:"total_transactions"`
		}
		body := map[string]any{
			"start_date": day.Format(from), "end_date": day.Format(to),
			"options": map[string]int{"count": pageSize, "offset": len(all)},
		}
		if err := c.post(ctx, "/transactions/get", body, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Transactions...)
		if len(page.Transactions) == 0 || len(all) >= page.Total {
			return all, nil
		}
	}
}

// post sends body with the credentials to path and decodes the reply into out. A failure
// reports Plaid's error code and message, never the request.
func (c *Client) post(ctx context.Context, path string, body map[string]any, out any) error {
	body["client_id"], body["secret"], body["access_token"] = c.ClientID, c.Secret, c.AccessToken
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("plaid %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Code    string `json:"error_code"`
			Message string `json:"error_message"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("plaid %s: %s: %s: %s", path, resp.Status, e.Code, e.Message)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("plaid %s: %w", path, err)
	}
	return nil
}

// Card is the item's card accounts as the trips layer sees them: Accounts maps each
// account's mask to the PocketSmith account it is.
type Card struct {
	Client   *Client
	Accounts map[string]string
}

// Records lists the posted transactions on the mapped accounts dated from to to.
func (c Card) Records(ctx context.Context, from, to time.Time) ([]trips.Record, error) {
	accounts, err := c.Client.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, a := range accounts {
		if name, ok := c.Accounts[a.Mask]; ok {
			names[a.ID] = name
		}
	}
	for mask := range c.Accounts {
		if !slices.ContainsFunc(accounts, func(a Account) bool { return a.Mask == mask }) {
			return nil, fmt.Errorf("plaid: no account with mask %s", mask)
		}
	}
	txs, err := c.Client.Transactions(ctx, from, to)
	if err != nil {
		return nil, err
	}
	var out []trips.Record
	for _, t := range txs {
		name, ok := names[t.AccountID]
		if !ok || t.Pending {
			continue
		}
		d, err := day.Parse(t.Date)
		if err != nil {
			return nil, fmt.Errorf("plaid transaction %s: %w", t.ID, err)
		}
		out = append(out, trips.Record{ID: t.ID, Account: name, Date: d, Amount: t.Amount, Name: t.Name,
			Merchant: t.Merchant, Code: t.Code, InStore: t.Channel == "in store"})
	}
	return out, nil
}
