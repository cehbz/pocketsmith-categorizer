// Package grab models what was bought from Grab.
package grab

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
)

// Kind is what a service delivers.
type Kind int

const (
	Ride Kind = iota + 1
	Delivery
)

// Service is the Grab service a payment was for.
type Service string

var kinds = map[Service]Kind{
	"Standard":            Ride,
	"Standard (JustGrab)": Ride,
	"JustGrab":            Ride,
	"JustGrab (SVB)":      Ride,
	"Premium":             Ride,
	"Taxi":                Ride,
	"RodDaeng":            Ride,
	"GrabBike":            Ride,
	"Bike":                Ride,
	"GrabFood":            Delivery,
	"GrabMart":            Delivery,
	"GrabExpress":         Delivery,
}

// aliases maps receipt label variants to their service.
var aliases = map[string]Service{
	"สั่งซื้อด้วย GrabMart": "GrabMart", // sang sue duay GrabMart: "order with GrabMart"
}

// ParseService normalizes a Grab service label.
func ParseService(label string) (Service, error) {
	label = strings.TrimSpace(label)
	if s, ok := aliases[label]; ok {
		return s, nil
	}
	if _, ok := kinds[Service(label)]; ok {
		return Service(label), nil
	}
	return "", fmt.Errorf("unknown Grab service %q", label)
}

// Kind classifies the service as a ride or a delivery.
func (s Service) Kind() Kind { return kinds[s] }

// Labels returns the English service labels, longest first.
func Labels() []string {
	ls := make([]string, 0, len(kinds))
	for s := range kinds {
		ls = append(ls, string(s))
	}
	slices.SortFunc(ls, func(a, b string) int {
		if d := len(b) - len(a); d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	return ls
}

// MaxPostingLag is how many days after its ledger date a payment's charge can post.
const MaxPostingLag = 3

// Payment is one charge Grab placed on the card: an order total or a tip.
type Payment struct {
	Booking string
	Service Service
	THB     float64
	At      time.Time // Bangkok time
	Account string    // whose Grab account; reporting only
}

// PostingWindow returns the first and last ledger dates in loc that a charge for p can carry.
func (p Payment) PostingWindow(loc *time.Location) (first, last time.Time) {
	first = day.Of(p.At, loc)
	return first, first.AddDate(0, 0, MaxPostingLag)
}
