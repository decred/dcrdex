package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"decred.org/dcrdex/client/asset/xmr/toolsdl"
	"decred.org/dcrdex/dex"
	"github.com/anaskhan96/soup"
	"github.com/decred/slog"
)

const moneroReleasePage = "https://github.com/monero-project/monero/releases"

func downloadAndParseText(url string) (string, error) {
	resp, err := soup.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download URL: %w", err)
	}
	doc := soup.HTMLParse(resp)
	cleanText := doc.FullText()
	return strings.TrimSpace(cleanText), nil
}

func scanText(txt string) ([]string, error) {
	const (
		downTrigger = "Download Hashes"
		likeTrigger = "If you would like to verify that you have downloaded the correct file"
		// state
		searching     = "searching"
		foundDownload = "foundDownload"
		collecting    = "collecting"
		// prefix
		monero = "monero-"
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
			if strings.Contains(line, downTrigger) {
				state = foundDownload
				continue
			}
		case foundDownload:
			if strings.Contains(line, likeTrigger) {
				state = collecting
				continue
			}
		case collecting:
			if strings.HasPrefix(line, monero) {
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
		tkns = strings.Split(zipLine, sepTypo) // missing ',' .. maybe html typo
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
	currVersion := make(toolsdl.AcceptableZipsList, 0) // safety, discarded; could also be a var
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
			currVersion = make(toolsdl.AcceptableZipsList, 0, 10)
		}

		currVersion = append(currVersion, *az)
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
	var log = dex.StdOutLogger("MKJSON", slog.LevelTrace)

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
		log.Errorf("error making versioned zip line lists: %v", err)
		os.Exit(1)
	}
	err = mkJson(versionedZips)
	if err != nil {
		log.Errorf("error marshaing json: %v", err)
		os.Exit(1)
	}
	log.Infof("encoded %d versions to JSON.", len(versionedZips))
}
