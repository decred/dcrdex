package core

import (
	"fmt"
	"testing"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

func TestArNfnt(t *testing.T) {
	tmpl := "تمت مراجعة حالة الطلب %v من %v إلى %v"
	localePrinter := message.NewPrinter(language.Arabic)
	t.Log(localePrinter.Sprintf(tmpl, "4442abababa2", "booked", "revoked"))
	fmt.Printf(tmpl+"\n", "4442abababa2", "booked", "revoked")
}
