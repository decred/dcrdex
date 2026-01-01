package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"decred.org/dcrdex/client/asset/xmr/toolsdl"
	"decred.org/dcrdex/dex"
	"github.com/PuerkitoBio/goquery"
	"github.com/decred/slog"
)

const (
	moneroReleasePage = "https://github.com/monero-project/monero/releases"
)

func downloadAndParseText(url string) (string, error) {
	// download the HTML content.
	res, err := http.Get(url) // default 20s is probably fine; if not lmk.
	if err != nil {
		return "", fmt.Errorf("failed to download URL: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bad status code: %d %s", res.StatusCode, res.Status)
	}

	// parse the HTML using 'goquery'.
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}
	// remove script and style elements to avoid including their code as text.
	doc.Find("script, style").Remove()
	// get the text of the entire body and trim whitespace.
	return strings.TrimSpace(doc.Text()), nil
}

func scanText(txt string) ([]string, error) {
	const (
		downStr = "Download Hashes"
		likeStr = "If you would like to verify that you have downloaded the correct file"
		// state
		searching     = "searching"
		foundDownload = "foundDownload"
		collecting    = "collecting"
	)

	var zipLines = make([]string, 0)

	r := strings.NewReader(txt)
	scanner := bufio.NewScanner(r)
	state := searching

	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 {
			continue
		}

		switch state {
		case searching:
			if strings.Contains(line, downStr) {
				state = foundDownload
				continue
			}
		case foundDownload:
			if strings.Contains(line, likeStr) {
				state = collecting
				continue
			}
		case collecting:
			if strings.HasPrefix(line, "monero-") {
				zipLines = append(zipLines, line)
				continue
			}
			state = searching
		}
	}

	return zipLines, scanner.Err()
}

var errSkip = errors.New("skip this line")

func getZipLinesTokens(zipLine string) (*toolsdl.AcceptableZip, error) {
	const (
		// separators
		sep     = ", "
		sepTypo = " "
		// extensions
		zip  = "zip"
		tbz2 = "tar.bz2"
	)

	tkns := strings.Split(zipLine, sep)
	if len(tkns) != 2 {
		tkns = strings.Split(zipLine, sepTypo) // missing ',' .. maybe typo
		if len(tkns) != 2 {
			return nil, fmt.Errorf("got %d tokens, expected 2", len(tkns))
		}
	}

	ext := ""
	if strings.HasSuffix(tkns[0], toolsdl.WinZipExt) {
		ext = zip
	} else if strings.HasSuffix(tkns[0], toolsdl.TarBz2Ext) {
		ext = tbz2
	} else {
		return nil, fmt.Errorf("zip has an invalid extension")
	}

	parts := strings.Split(tkns[0], toolsdl.Dash)
	lenParts := len(parts)
	if lenParts <= 3 { // source code zipLine or bad line
		return nil, errSkip
	}
	if lenParts > 4 {
		return nil, fmt.Errorf("got %d parts, expected 4", lenParts)
	}
	dirPath := toolsdl.GetDirFromZip(tkns[0])

	var az = &toolsdl.AcceptableZip{
		Zip:  tkns[0],
		Hash: tkns[1],
		Dir:  dirPath,
		Ext:  ext,
		Os:   parts[1],
		Arch: parts[2],
	}

	return az, nil
}

func makeVersionedZips(zipLines []string) ([]toolsdl.AcceptableZipsList, error) {
	var zipVersions []toolsdl.AcceptableZipsList

	var currentDir = ""
	currVersion := make(toolsdl.AcceptableZipsList, 0)
	for i, zipLine := range zipLines {
		az, err := getZipLinesTokens(zipLine)
		if err != nil {
			if errors.Is(errSkip, err) {
				continue
			}
			return nil, err
		}

		dir := az.Dir[strings.LastIndex(az.Dir, toolsdl.Dash)+1:] // '-v0.x.x.x'

		if dir != currentDir {
			currentDir = dir
			if i > 0 {
				zipVersions = append(zipVersions, currVersion)
			}
			currVersion = make(toolsdl.AcceptableZipsList, 0)
		}

		currVersion = append(currVersion, *az)
	}

	for _, zvs := range zipVersions {
		// fmt.Printf("%s\n\n", zvs)
		for _, zv := range zvs {
			fmt.Println(zv)
		}
		fmt.Println()

	}

	return zipVersions, nil
}

func mkJson(acceptableZipsLists []toolsdl.AcceptableZipsList) error {
	var vs = toolsdl.VersionSet{}
	for _, azs := range acceptableZipsLists {
		v := toolsdl.Version{
			AcceptableZips: azs,
		}
		vs.Versions = append(vs.Versions, v)
	}
	b, err := json.MarshalIndent(vs, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", b)
	return nil
}

func main() {
	var log = dex.StdOutLogger("Test", slog.LevelTrace)

	txtReleasesPage, err := downloadAndParseText(moneroReleasePage)
	if err != nil {
		log.Errorf("error downloading or parsing page text: %v", err)
		os.Exit(1)
	}
	zipLines, err := scanText(txtReleasesPage)
	if err != nil {
		log.Errorf("error scanning text: %v", err)
		os.Exit(1)
	}
	versionedZips, err := makeVersionedZips(zipLines)
	if err != nil {
		log.Errorf("error making versioned zip line list: %v", err)
		os.Exit(1)
	}

	// err = mkJson(verList)
	// if err != nil {
	// 	log.Errorf("error making json: %v", err)
	// 	os.Exit(1)
	// }
	log.Infof("done %d versions.", len(versionedZips))
}
