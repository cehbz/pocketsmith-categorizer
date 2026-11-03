package trips

import (
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// charge is a card charge whose record names merchant.
func charge(id int64, d string, amount float64, merchant string) Transaction {
	return Transaction{Transaction: ledger.Transaction{ID: id, Date: date(d), Amount: amount, Payee: merchant + " 1234"}, Details: Details{Merchant: merchant}}
}

func TestTheMerchantIsTheRecordsNameElseThePayeeKey(t *testing.T) {
	ticket := Transaction{Transaction: ledger.Transaction{Payee: "AIRLINE 7Z9XYZ"}, Details: Details{Merchant: "Airline"}}
	refund := Transaction{Transaction: ledger.Transaction{Payee: "AIRLINE"}, Details: Details{Merchant: "Airline"}}
	if ticket.Merchant() != "AIRLINE" || refund.Merchant() != "AIRLINE" {
		t.Errorf("merchants = %q, %q, want AIRLINE", ticket.Merchant(), refund.Merchant())
	}
	if got := (Transaction{Transaction: ledger.Transaction{Payee: "GOOGLE *FI AbCDE1"}}).Merchant(); got != "GOOGLE FI" {
		t.Errorf("unmatched merchant = %q, want GOOGLE FI", got)
	}
	if got := (Transaction{Transaction: ledger.Transaction{Payee: "12345"}}).Merchant(); got != "12345" {
		t.Errorf("merchant without letters = %q, want 12345", got)
	}
}

func TestThePayeeKeySkipsTokensWithThreeOrMoreDigits(t *testing.T) {
	for payee, want := range map[string]string{
		"xx20000Q9C7777 ALPHA PLAZA HOMETOWN": "ALPHA PLAZA",
		"xx20000Q9C8888R BETA MART HOMETOWN":  "BETA MART",
		"xx20000Q9C9999 GAMMA BANK FARCITY":   "GAMMA BANK",
		"AIRLINE 0123456789":                  "AIRLINE",
		"7-ELEVEN 54321 FARCITY":              "ELEVEN FARCITY",
		"12GO FARCITY":                        "GO FARCITY",
		"98765":                               "98765",
		"AB123 CAFE FARCITY":                  "CAFE FARCITY", // exactly three digits is skipped
		"AB12 CAFE FARCITY":                   "AB CAFE",      // two digits stays in the key
		"AB123 456":                           "AB",           // every token skipped: the whole payee's key
	} {
		if got := (Transaction{Transaction: ledger.Transaction{Payee: payee}}).Merchant(); got != want {
			t.Errorf("merchant of %q = %q, want %q", payee, got, want)
		}
	}
}
