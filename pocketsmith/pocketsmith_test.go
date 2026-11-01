package pocketsmith

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	api "github.com/cehbz/pocketsmith"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

type fakeAPI struct {
	txs     []api.Transaction
	cats    []api.Category
	query   api.Query
	updated map[int64]api.Update
	zone    *time.Location
}

func (f *fakeAPI) Profile(ctx context.Context) (api.Profile, error) {
	return api.Profile{ID: 1, TimeZone: f.zone}, nil
}

func (f *fakeAPI) Transactions(ctx context.Context, q api.Query) ([]api.Transaction, error) {
	f.query = q
	return f.txs, nil
}

func (f *fakeAPI) Transaction(ctx context.Context, id int64) (api.Transaction, error) {
	for _, t := range f.txs {
		if t.ID == id {
			return t, nil
		}
	}
	return api.Transaction{}, nil
}

func (f *fakeAPI) Update(ctx context.Context, id int64, u api.Update) error {
	if f.updated == nil {
		f.updated = map[int64]api.Update{}
	}
	f.updated[id] = u
	return nil
}

func mustDay(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := day.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestChargesQueriesTheAccountAndKeepsThePayees(t *testing.T) {
	f := &fakeAPI{txs: []api.Transaction{
		{ID: 1, Date: mustDay(t, "2026-01-12"), Amount: -2.50, Payee: "WWW.GRAB.COM", Category: &api.Category{ID: 9}, Labels: []string{"x"}},
		{ID: 2, Date: mustDay(t, "2026-01-12"), Amount: -40, Payee: "SOMEWHERE ELSE", Category: &api.Category{ID: 4}},
		{ID: 3, Date: mustDay(t, "2026-01-13"), Amount: -1.25, Payee: "GRABTAXI", NeedsReview: true},
	}}
	l := &Ledger{API: f}
	first, last := mustDay(t, "2026-01-01"), mustDay(t, "2026-01-31")
	got, err := l.Charges(context.Background(), 7, first, last, []string{"WWW.GRAB.COM", "GRABTAXI"})
	if err != nil {
		t.Fatal(err)
	}
	if f.query.Account != 7 || f.query.User != 0 || !f.query.From.Equal(first) || !f.query.To.Equal(last) || f.query.NeedsReview {
		t.Errorf("query = %+v", f.query)
	}
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 3 {
		t.Fatalf("charges = %+v, want IDs 1 and 3", got)
	}
	if got[0].USD != 2.50 || day.Format(got[0].Date) != "2026-01-12" || got[0].Payee != "WWW.GRAB.COM" {
		t.Errorf("charge 1 = %+v", got[0])
	}
	if !got[0].State.Equal(ledger.State{Category: 9, Labels: []string{"x"}}) {
		t.Errorf("charge 1 state = %+v", got[0].State)
	}
	if got[1].State.Category != 0 || !got[1].State.NeedsReview {
		t.Errorf("uncategorized charge 3 state = %+v", got[1].State)
	}
}

func TestChargeTranslatesOneTransaction(t *testing.T) {
	f := &fakeAPI{txs: []api.Transaction{{ID: 5, Date: mustDay(t, "2026-01-12"), Amount: -2.75, Payee: "WWW.GRAB.COM", Category: &api.Category{ID: 7}, Labels: []string{"grab-auto"}}}}
	got, err := (&Ledger{API: f}).Charge(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 5 || got.USD != 2.75 || !got.State.Equal(ledger.State{Category: 7, Labels: []string{"grab-auto"}}) {
		t.Errorf("Charge = %+v", got)
	}
}

func TestUpdateSendsTheState(t *testing.T) {
	f := &fakeAPI{}
	l := &Ledger{API: f}
	if err := l.Update(context.Background(), 5, ledger.State{Category: 7, NeedsReview: false, Labels: []string{"x", "grab-auto"}}); err != nil {
		t.Fatal(err)
	}
	if err := l.Update(context.Background(), 6, ledger.State{Category: 0, NeedsReview: true}); err != nil {
		t.Fatal(err)
	}
	u5, u6 := f.updated[5], f.updated[6]
	if u5.Category != 7 || u5.NeedsReview || len(u5.Labels) != 2 || u5.Labels[1] != "grab-auto" {
		t.Errorf("update 5 = %+v", u5)
	}
	if u6.Category != 0 || !u6.NeedsReview || len(u6.Labels) != 0 {
		t.Errorf("update 6 = %+v", u6)
	}
}

func TestTimeZoneIsTheProfileZone(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	loc, err := (&Ledger{API: &fakeAPI{zone: ny}}).TimeZone(context.Background())
	if err != nil || loc != ny {
		t.Errorf("TimeZone = %v, %v", loc, err)
	}
}

func (f *fakeAPI) Categories(ctx context.Context, user int64) ([]api.Category, error) {
	if user != 1 {
		return nil, fmt.Errorf("user %d, want the profile's 1", user)
	}
	return f.cats, nil
}

func TestTransactionsQueriesTheUser(t *testing.T) {
	f := &fakeAPI{txs: []api.Transaction{
		{ID: 1, Date: mustDay(t, "2026-01-12"), Amount: -2.50, Payee: "CAFE", Category: &api.Category{ID: 9}, NeedsReview: true, Labels: []string{"x"}, Account: api.Account{ID: 7, Name: "Card"}},
	}}
	first, last := mustDay(t, "2026-01-01"), mustDay(t, "2026-01-31")
	got, err := (&Ledger{API: f}).Transactions(context.Background(), first, last, true)
	if err != nil {
		t.Fatal(err)
	}
	if f.query.User != 1 || f.query.Account != 0 || !f.query.From.Equal(first) || !f.query.To.Equal(last) || !f.query.NeedsReview {
		t.Errorf("query = %+v", f.query)
	}
	want := ledger.Transaction{ID: 1, Date: mustDay(t, "2026-01-12"), Amount: -2.50, Payee: "CAFE", Account: ledger.Account{ID: 7, Name: "Card"},
		State: ledger.State{Category: 9, NeedsReview: true, Labels: []string{"x"}}}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Errorf("Transactions = %+v, want %+v", got, want)
	}
}

func TestCategoriesTranslates(t *testing.T) {
	f := &fakeAPI{cats: []api.Category{{ID: 4, Title: "Rent"}, {ID: 9, Title: "Eating Out"}}}
	got, err := (&Ledger{API: f}).Categories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []ledger.Category{{ID: 4, Title: "Rent"}, {ID: 9, Title: "Eating Out"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Categories = %+v, want %+v", got, want)
	}
}
