package trips

import (
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

func date(s string) time.Time {
	d, err := day.Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

func ltx(id int64, d string, amount float64, payee, account string) ledger.Transaction {
	return ledger.Transaction{ID: id, Date: date(d), Amount: amount, Payee: payee, Account: ledger.Account{Name: account}}
}

func rec(id, d string, amount float64, name, merchant string) Record {
	return Record{ID: id, Account: "Card", Date: date(d), Amount: amount, Name: name, Merchant: merchant, Code: "5812", InStore: true}
}

func TestMatchPairsTheSameCentsWithinTheWindow(t *testing.T) {
	txs := []ledger.Transaction{ltx(1, "2026-03-10", -12.50, "CAFE", "Card"), ltx(2, "2026-03-10", 5, "CAFE REFUND", "Card")}
	recs := []Record{rec("a", "2026-03-15", 12.50, "CAFE", "Cafe"), rec("b", "2026-03-08", -5, "CAFE", "Cafe")}
	got := Match(txs, recs)
	if got[1] != (Evidence{Merchant: "Cafe", Code: "5812", InStore: true}) || got[2].Merchant != "Cafe" {
		t.Errorf("Match = %+v, want both matched", got)
	}
}

func TestMatchSkipsOtherDatesAmountsAndSigns(t *testing.T) {
	txs := []ledger.Transaction{ltx(1, "2026-03-10", -12.50, "CAFE", "Card")}
	recs := []Record{
		rec("early", "2026-03-07", 12.50, "CAFE", "Cafe"),
		rec("late", "2026-03-16", 12.50, "CAFE", "Cafe"),
		rec("amount", "2026-03-10", 12.51, "CAFE", "Cafe"),
		rec("sign", "2026-03-10", -12.50, "CAFE", "Cafe"),
	}
	if got := Match(txs, recs); len(got) != 0 {
		t.Errorf("Match = %+v, want none", got)
	}
}

func TestMatchKeepsToTheRecordsAccount(t *testing.T) {
	got := Match([]ledger.Transaction{ltx(1, "2026-03-10", -12.50, "CAFE", "Checking")}, []Record{rec("a", "2026-03-10", 12.50, "CAFE", "Cafe")})
	if len(got) != 0 {
		t.Errorf("Match = %+v, want none", got)
	}
}

func TestMatchPrefersSharedPayeeWordsThenTheNearestDate(t *testing.T) {
	txs := []ledger.Transaction{ltx(1, "2026-03-10", -9, "CAFE ONE BIGCITY", "Card"), ltx(2, "2026-03-10", -4, "SHOP", "Card"), ltx(3, "2026-03-10", -6, "KIOSK", "Card")}
	recs := []Record{
		rec("near", "2026-03-10", 9, "OTHER PLACE", "Other"),
		rec("words", "2026-03-13", 9, "CAFE ONE", "Cafe One"),
		rec("far", "2026-03-14", 4, "SHOP", "Far Shop"),
		rec("close", "2026-03-11", 4, "SHOP", "Close Shop"),
		rec("before", "2026-03-08", 6, "KIOSK", "Far Kiosk"), // earlier, so first by date, but farther
		rec("after", "2026-03-11", 6, "KIOSK", "Near Kiosk"),
	}
	got := Match(txs, recs)
	if got[1].Merchant != "Cafe One" || got[2].Merchant != "Close Shop" || got[3].Merchant != "Near Kiosk" {
		t.Errorf("Match = %+v, want Cafe One, Close Shop and Near Kiosk", got)
	}
}

func TestMatchUsesARecordOnce(t *testing.T) {
	txs := []ledger.Transaction{ltx(1, "2026-03-10", -7, "SHOP", "Card"), ltx(2, "2026-03-11", -7, "SHOP", "Card")}
	got := Match(txs, []Record{rec("a", "2026-03-11", 7, "SHOP", "Shop")})
	if _, ok := got[1]; !ok || len(got) != 1 {
		t.Errorf("Match = %+v, want only the earlier transaction matched", got)
	}
}
