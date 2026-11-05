package grab

import "github.com/cehbz/pocketsmith-categorizer/evidence"

// Query finds Grab's e-receipts.
const Query = `from:no-reply@grab.com subject:"Your Grab E-Receipt"`

// Label marks the charges Grab's receipts decided.
const Label = "grab-auto"

// deliveries are the kind of part each delivery service sells; every other service is a ride.
var deliveries = map[Service]evidence.Kind{"GrabFood": evidence.Food, "GrabMart": evidence.Mart, "GrabExpress": evidence.Express}

// Part is the kind of part the service sells.
func (s Service) Part() evidence.Kind {
	if k, ok := deliveries[s]; ok {
		return k
	}
	return evidence.Ride
}

// Reader reads Grab's e-receipts.
type Reader struct{}

// Read reads the payment an e-receipt records as a receipt of one part, paid when the
// receipt arrived. A tip is its own receipt, of the kind of the service it tips.
func (Reader) Read(m evidence.Message) (evidence.Evidence, error) {
	p, err := ReadReceipt(m.Text, m.Received, "")
	if err != nil {
		return evidence.Evidence{}, err
	}
	part := evidence.Part{Ref: p.Booking, Kind: p.Service.Part(), THB: p.THB}
	return evidence.Evidence{Receipts: []evidence.Receipt{{At: p.At, Parts: []evidence.Part{part}}}}, nil
}

// Merchant is Grab as the receipt layer knows it, before the user's config gives its payees,
// its receipts' start and its categories. Its charges post up to MaxPostingLag days after the
// receipt, within 2% of the receipt's amount.
func Merchant() evidence.Merchant {
	return evidence.Merchant{Name: "grab", Label: Label, Queries: []string{Query},
		Kinds:     []evidence.Kind{evidence.Ride, evidence.Food, evidence.Mart, evidence.Express},
		Tolerance: 0.02, Lag: MaxPostingLag, Reader: Reader{}}
}
