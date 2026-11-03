package plaid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

func mustDay(s string) time.Time {
	d, err := day.Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

// server is a fake Plaid with two accounts, answering transactions two to a page; bodies
// records each request's body. It reports total_transactions one over what it holds, so
// only an empty page ends the read, and fails any request after that one with an error, so
// a loop that does not end fails an assertion instead of hanging.
func server(t *testing.T, txs []Transaction, pageSize int) (c *Client, bodies *[]map[string]any) {
	t.Helper()
	bodies = &[]map[string]any{}
	pages := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		*bodies = append(*bodies, body)
		switch r.URL.Path {
		case "/accounts/get":
			json.NewEncoder(w).Encode(map[string]any{"accounts": []Account{{ID: "acc-1", Mask: "1111", Name: "Card One"}, {ID: "acc-2", Mask: "2222", Name: "Card Two"}}})
		case "/transactions/get":
			if pages++; pages > (len(txs)+pageSize-1)/pageSize+1 {
				t.Errorf("transactions request %d: the read should have ended", pages)
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error_code":"TOO_MANY","error_message":"too many pages"}`))
				return
			}
			offset := int(body["options"].(map[string]any)["offset"].(float64))
			end := min(offset+pageSize, len(txs))
			json.NewEncoder(w).Encode(map[string]any{"transactions": txs[min(offset, end):end], "total_transactions": len(txs) + 1})
		default:
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error_code":"NOT_FOUND","error_message":"no such path"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, ClientID: "id", Secret: "s3cret", AccessToken: "tok3n", HTTP: srv.Client()}, bodies
}

func TestTransactionsPagesByOffsetWithTheCredentials(t *testing.T) {
	c, bodies := server(t, []Transaction{{ID: "t1"}, {ID: "t2"}, {ID: "t3"}}, 2)
	got, err := c.Transactions(context.Background(), mustDay("2026-01-01"), mustDay("2026-01-31"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[2].ID != "t3" {
		t.Errorf("Transactions = %+v, want t1..t3", got)
	}
	// The fake's total is one more than it holds, so the third request gets an empty page.
	if len(*bodies) != 3 {
		t.Fatalf("requests = %d, want 3", len(*bodies))
	}
	for i, b := range *bodies {
		opts := b["options"].(map[string]any)
		if b["client_id"] != "id" || b["secret"] != "s3cret" || b["access_token"] != "tok3n" || b["start_date"] != "2026-01-01" ||
			b["end_date"] != "2026-01-31" || opts["count"] != float64(500) || opts["offset"] != float64(min(2*i, 3)) {
			t.Errorf("request %d = %v", i, b)
		}
	}
}

func TestTransactionsStopsOnAnEmptyPage(t *testing.T) {
	c, bodies := server(t, nil, 2)
	got, err := c.Transactions(context.Background(), mustDay("2026-01-01"), mustDay("2026-01-31"))
	if err != nil || len(got) != 0 || len(*bodies) != 1 {
		t.Errorf("Transactions = %v, %v after %d requests, want none after 1", got, err, len(*bodies))
	}
}

func TestRecordsKeepsThePostedTransactionsOfTheMappedAccounts(t *testing.T) {
	txs := []Transaction{
		{ID: "t1", AccountID: "acc-1", Date: "2026-01-05", Amount: 12.5, Name: "CAFE 123", Merchant: "Cafe", Code: "5812", Channel: "in store"},
		{ID: "t2", AccountID: "acc-1", Date: "2026-01-06", Amount: -3, Name: "REFUND", Channel: "online"},
		{ID: "t3", AccountID: "acc-2", Date: "2026-01-06", Amount: 9, Name: "OTHER CARD"},
		{ID: "t4", AccountID: "acc-1", Date: "2026-01-07", Amount: 4, Name: "PENDING", Pending: true},
	}
	c, _ := server(t, txs, 500)
	got, err := Card{Client: c, Accounts: map[string]string{"1111": "Card"}}.Records(context.Background(), mustDay("2026-01-01"), mustDay("2026-01-31"))
	if err != nil {
		t.Fatal(err)
	}
	want := []trips.Record{
		{ID: "t1", Account: "Card", Date: mustDay("2026-01-05"), Amount: 12.5, Name: "CAFE 123", Merchant: "Cafe", Code: "5812", InStore: true},
		{ID: "t2", Account: "Card", Date: mustDay("2026-01-06"), Amount: -3, Name: "REFUND"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Records = %+v, want %+v", got, want)
	}
}

func TestRecordsRefusesAMaskWithNoAccount(t *testing.T) {
	c, _ := server(t, nil, 500)
	if _, err := (Card{Client: c, Accounts: map[string]string{"9999": "Card"}}).Records(context.Background(), mustDay("2026-01-01"), mustDay("2026-01-31")); err == nil {
		t.Error("Records accepted a mask no account has")
	}
}

func TestAnAPIErrorFailsTheReadWithoutTheCredentials(t *testing.T) {
	c, _ := server(t, nil, 500)
	c.BaseURL += "/missing"
	_, err := c.Accounts(context.Background())
	if err == nil || !strings.Contains(err.Error(), "NOT_FOUND") || strings.Contains(err.Error(), "s3cret") || strings.Contains(err.Error(), "tok3n") {
		t.Errorf("Accounts error = %v, want Plaid's error code without the credentials", err)
	}
}
