package main

import (
	"fmt"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

const key = "key"

func main() {
	icelandicTmpl := "%.2f gígavött"
	englishTmpl := "%.2f gigawatts"

	err := message.SetString(language.Icelandic, key, icelandicTmpl)
	if err != nil {
		panic(err.Error())
	}
	err = message.SetString(language.AmericanEnglish, key, englishTmpl)
	if err != nil {
		panic(err.Error())
	}

	icelandicPrinter := message.NewPrinter(language.Icelandic)
	englishPrinter := message.NewPrinter(language.AmericanEnglish)

	fmt.Println("Icelandic (using key):", icelandicPrinter.Sprintf(key, 1.21))
	fmt.Println("Icelandic (direct):   ", icelandicPrinter.Sprintf(icelandicTmpl, 1.21))

	fmt.Println("English (using key):  ", englishPrinter.Sprintf(key, 1.21))
	fmt.Println("English (direct):     ", englishPrinter.Sprintf(englishTmpl, 1.21))
}
