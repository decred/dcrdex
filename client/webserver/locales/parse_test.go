package locales

import (
	"reflect"
	"slices"
	"testing"
)

func TestTokens(t *testing.T) {
	tmplFile := []byte(`
{{define "settings"}}
{{template "top" .}}
<div id="main" data-handler="settings" class="text-center py-5 overflow-y-auto">
  <div class="settings">
    <div class="form-check">
      <input class="form-check-input" type="checkbox" value="" id="showPokes" checked>
      <label class="form-check-label" for="showPokes">
        [[[Show pop-up notifications]]]
      </label>
    </div>
    <div id="fiatRateSources" {{if not .UserInfo.Authed}} class="d-hide"{{end}}>
    <span class="mb-2" data-tooltip="[[[fiat_exchange_rate_msg]]]">
     [[[fiat_exchange_rate_sources]]]:
    <span class="ico-info"></span>
    </span>
    <div>
      <br>
      <div {{if not .UserInfo.Authed}} class="d-hide"{{end}}>
        <p>
        [[[simultaneous_servers_msg]]]
        </p>
        <button id="addADex" class="bg2 selected">[[[Add a DEX]]]</button>
        <button id="importAccount" class="bg2 selected ms-2">[[[Import Account]]]</button>
      </div>
    </div>
</div>
{{template "bottom"}}
{{end}}
[[[ and Lets tRy: a_different  _HARDER_  .pattern. ::.-_-. ]]]
[[[this shouldn't be included because of this number here: 1]]]
`)
	wantTokens := []string{
		"[[[Show pop-up notifications]]]",
		"[[[fiat_exchange_rate_msg]]]",
		"[[[fiat_exchange_rate_sources]]]",
		"[[[simultaneous_servers_msg]]]",
		"[[[Add a DEX]]]",
		"[[[Import Account]]]",
		"[[[ and Lets tRy: a_different  _HARDER_  .pattern. ::.-_-. ]]]",
	}
	slices.Sort(wantTokens)
	wantKeys := []string{
		"Show pop-up notifications",
		"fiat_exchange_rate_msg",
		"fiat_exchange_rate_sources",
		"simultaneous_servers_msg",
		"Add a DEX",
		"Import Account",
		" and Lets tRy: a_different  _HARDER_  .pattern. ::.-_-. ",
	}
	slices.Sort(wantKeys)

	got := Tokens(tmplFile)
	var (
		gotTokens []string
		gotKeys   []string
	)
	for _, matchGroup := range got {
		if len(matchGroup) != 2 {
			t.Fatalf("can't parse match group: %v", matchGroup)
		}
		token, key := string(matchGroup[0]), string(matchGroup[1])
		gotTokens = append(gotTokens, token)
		gotKeys = append(gotKeys, key)
	}
	slices.Sort(gotTokens)
	slices.Sort(gotKeys)

	if !reflect.DeepEqual(wantTokens, gotTokens) {
		t.Fatalf("expected tokens: %+v, got: %+v", wantTokens, gotTokens)
	}
	if !reflect.DeepEqual(wantKeys, gotKeys) {
		t.Fatalf("expected keys: %+v, got: %+v", wantKeys, gotKeys)
	}
}
