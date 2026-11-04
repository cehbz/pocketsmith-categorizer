package grab

import (
	"testing"
	"time"
)

const (
	rideEN = `Grab E-Receipt | Standard | | Hope you enjoyed your ride! Picked up on 02 November 2026 Booking ID: A-TESTRIDE0001 | | Total Paid | ฿78 | Breakdown | Fare | 56 | Platform Fee | 20 | Foreign payment fee | 2 | Total Paid | 78 | Your Trip 2.4 km • 6 mins | Example Mall 1:12PM | Example Cafe 1:19PM | Grab Thailand, Bangkok 10110`
	tipEN  = `Standard Thanks! Your tip goes a long way for your driver. 03 Nov 26 12:09 +0700 Total Paid ฿60 Tip From: Example Rd, Hometown, Hometown, 10000, Thailand Booking ID: A-TESTTIP00002`
	foodTH = `| ทานอาหารให้อร่อย! | | รวม ฿ 387 | วันที่ | เวลา 27 Oct 26 08:42 +0700 | ใบเสร็จรับเงิน | GrabFood | ชื่อผู้เดินทาง Test | รหัสการจอง A-TESTFOOD0003 | 1x Sandwich | ฿ 189 | ค่าอาหาร | ฿ 418 | Free deli ฿10 off, min spend ฿100 | - ฿ 10 | รวม | ฿ 387 | Copyright @ 2026 GrabTaxi Holdings Pte. Ltd.`
	martTH = `ขอบคุณที่ซื้อสินค้ากับเรา! ทั้งหมด ฿ 320 จัดส่งเมื่อ 29 Oct 26 15:31 +0700 ใบเสร็จรับเงิน รายละเอียดคำสั่งซื้อ สั่งซื้อด้วย GrabMart สั่งซื้อโดย Test รหัสคำสั่งซื้อ A-TESTMART0004`
	tipTH  = ` Grab Tips E-Receipt | GrabMart | | ขอบคุณสำหรับทิปเพื่อเป็นกำลังใจให้คนขับ! 01 Nov 26 11:50 +0700 | | รวมทั้งหมด | THB 50.00 | ค่าทิป รหัสการจอง: A-TESTTIPTH005 | 50.00 | รวมทั้งหมด | 50.00`
)

func TestReadReceipt(t *testing.T) {
	at := time.Date(2026, 11, 2, 6, 19, 0, 0, time.UTC)
	for _, c := range []struct {
		name, text string
		service    Service
		thb        float64
		booking    string
	}{
		{"english ride", rideEN, "Standard", 78, "A-TESTRIDE0001"},
		{"english tip", tipEN, "Standard", 60, "A-TESTTIP00002"},
		{"thai food order, promotions after the total, GrabTaxi footer", foodTH, "GrabFood", 387, "A-TESTFOOD0003"},
		{"thai mart order", martTH, "GrabMart", 320, "A-TESTMART0004"},
		{"thai tip", tipTH, "GrabMart", 50, "A-TESTTIPTH005"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := ReadReceipt(c.text, at, "a@example.com")
			if err != nil {
				t.Fatal(err)
			}
			want := Payment{Booking: c.booking, Service: c.service, THB: c.thb, At: at, Account: "a@example.com"}
			if got != want {
				t.Errorf("ReadReceipt = %+v, want %+v", got, want)
			}
		})
	}
}

func TestReadReceiptUnreadable(t *testing.T) {
	for _, text := range []string{
		`Hope you enjoyed your ride! Total Paid ฿78`,                      // no service
		`Standard Hope you enjoyed your ride! Booking ID: A-TESTRIDE0001`, // no total
	} {
		if _, err := ReadReceipt(text, time.Time{}, ""); err == nil {
			t.Errorf("ReadReceipt(%q) read a payment", text)
		}
	}
}
