package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/i18n"
	"golang.org/x/text/language"
)

var (
	m map[string]interface{} = map[string]interface{}{
		"enus": i18n.EnUS,
		"ptbr": i18n.PtBr,
	}
)

var translator = i18n.NewPackageTranslator("core", language.AmericanEnglish)

func registerLocale(lang language.Tag, dict map[core.Topic]*i18n.Translation) {
	t := translator.LanguageTranslator(lang)
	for topic, tln := range dict {
		t.RegisterNotifications(string(topic), tln)
	}
}

func handleGoDataReq(w http.ResponseWriter, r *http.Request) {
	// Set the response header to JSON
	w.Header().Set("Content-Type", "application/json")

	lang := r.FormValue("lang")
	// Encode the data as JSON and write it to the response
	json.NewEncoder(w).Encode(m[lang])
}

func handleGoNtfnDataReq(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	lang := r.FormValue("lang")
	acceptLang, err := language.Parse(lang)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dict := translator.GetDict(acceptLang)
	jsonData, err := json.Marshal(dict)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Encode the data as JSON and write it to the response
	w.Write(jsonData)
}

func handleGoNtfnDocDataReq(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	documentedDict := translator.GetDocumentedDict()
	// for id, docTranslated := range documentedDict {
	// 	fmt.Printf("id: %+v | doc: %+v\n", id, docTranslated)
	// 	fmt.Printf("Translation: %+v \n\n", docTranslated.Translation)
	// }
	jsonData, err := json.Marshal(documentedDict)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Encode the data as JSON and write it to the response
	w.Write(jsonData)
}

func main() {
	for topic, tln := range core.OriginLocale {
		translator.RegisterNotifications(string(topic), tln)
	}
	registerLocale(language.BrazilianPortuguese, core.PTBR)

	// registerLocale(language.SimplifiedChinese, zhCN)
	// registerLocale(language.Polish, plPL)
	// registerLocale(language.German, deDE)

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})
	http.HandleFunc("/translate-js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "translator.html")
	})
	http.HandleFunc("/translate-go", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "translator-go.html")
	})

	http.HandleFunc("/translate-ntfn", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "translator-ntfn.html")
	})
	http.HandleFunc("/data", handleGoDataReq)
	http.HandleFunc("/data-ntfn", handleGoNtfnDataReq)
	http.HandleFunc("/data-ntfn-doc", handleGoNtfnDocDataReq)

	fmt.Println("Listening on :8080")
	http.ListenAndServe(":8080", nil)
}
