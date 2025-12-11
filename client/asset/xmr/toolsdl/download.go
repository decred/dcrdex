//////////////////////////////////////////////////////////////////////////go:build xmrlive

package toolsdl

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/bzip2"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"decred.org/dcrdex/dex"
)

// The toolsdl package is a method to download monero-wallet-cli and monero-wallet-rpc
// binaries from getmonero.org.
//
// First the canonical signed hashes.txt file is downloaded and  checked for version and
// if needed compared to current. Then the list of zipfiles with hashes is examined for the
// one containing the hashed zipfile for the architecture the end user is on.
//
// That file (tar/bz2 or zip) is checked against it's sha256 hash. If correct the file is
// decompressed and the 2 specific binaries above are extracted to the user directory
// from where they are subsequently run.

const (
	HashesLink                = "https://www.getmonero.org/downloads/hashes.txt"
	MoneroHashesFilename      = "hashes.txt"
	DexMoneroUserToolsPathDir = ".dex-monero-tools"
)

func getToolsBasePath() (string, error) {
	userPath, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userPath, DexMoneroUserToolsPathDir), nil
}

type machine struct {
	arch string
	os   string
}

func getMachine() *machine {
	return &machine{
		os:   runtime.GOOS,
		arch: runtime.GOARCH,
	}
}

const (
	CliTrigger        = "## CLI"
	HashZipTerminator = "#"
	TwoSpaces         = "  "
	Dash              = "-"
	Dot               = "."
)

// represents a '[sha hash] [monero-<os>-<arch>-<version>.<extension>]' line
// from getmonero.org/downloads/hashes.txt
type hashedZip struct {
	hash     string
	zip      string
	os       string
	arch     string
	ext      string
	valid    bool
	selected bool
	version  *moneroVersionV0
}

type hashedZips []hashedZip

type Download struct {
	m   *machine
	z   hashedZips
	log dex.Logger
}

func (d *Download) SetLogger(logger dex.Logger) {
	d.log = logger.SubLogger("DLTL")
}

var ErrNoLocalVersion = errors.New("cannot find current version in local tools path")
var ErrNoRemoteVersion = errors.New("cannot find appropriate tools version in hashes.txt")

// GetCurrentLocalToolsDir retreives latest downloaded and running version directory
// from ToolsPath.
func (d *Download) GetCurrentLocalToolsDir() (string, string, error) {
	toolsPath, err := getToolsBasePath()
	if err != nil {
		return "", "", ErrNoLocalVersion
	}
	entries, err := os.ReadDir(toolsPath)
	if err != nil {
		return "", "", ErrNoLocalVersion
	}
	for _, entry := range entries {
		if entry.IsDir() {
			_, ok := strings.CutPrefix(entry.Name(), "monero")
			if ok {
				return filepath.Join(toolsPath, entry.Name()), entry.Name(), nil
			}
		}
	}
	return "", "", ErrNoLocalVersion
}

// ========================================

// GetLatestRemoteCanonicalVersion downloads hashes.txt from getmonero.org and
// checks it's version.
func (d *Download) GetLatestRemoteCanonicalVersion(ctx context.Context) (*moneroVersionV0, error) {
	d.m = getMachine()

	hashFilePath, err := downloadHashesFile(ctx)
	if err != nil {
		return nil, err
	}
	err = d.getHashedZips(hashFilePath)
	if err != nil {
		return nil, err
	}
	err = d.checkHashedZips()
	if err != nil {
		return nil, err
	}
	err = d.chooseHashedZip()
	if err != nil {
		return nil, err
	}
	for i, hzip := range d.z {
		if hzip.valid && hzip.selected {
			if d.z[i].version != nil {
				return d.z[i].version, nil
			}
		}
	}
	return nil, ErrNoRemoteVersion
}

// ========================================

// Run performs a full download and check of the latest canonical version of the
// monero-wallet-cli & monero-wallet-rpc tools and installs in ToolsDir if valid.
func (d *Download) Run(ctx context.Context) (string, error) {
	d.m = getMachine()
	d.cleanAll()

	hashFilePath, err := downloadHashesFile(context.Background())
	if err != nil {
		return "", err
	}
	d.log.Trace("Hashes file downloaded")
	err = d.getHashedZips(hashFilePath)
	if err != nil {
		return "", err
	}
	d.log.Trace("Collected hashed zips")
	err = d.checkHashedZips()
	if err != nil {
		return "", err
	}
	d.log.Trace("Checked hashed zips")
	err = d.chooseHashedZip()
	if err != nil {
		return "", err
	}
	d.log.Tracef("Chose zip for %s %s", d.m.os, d.m.arch)

	// zip selected
	fp, err := d.downloadFile(context.Background())
	if err != nil {
		d.cleanAll()
		return "", err
	}
	d.log.Trace("Downloaded zip file")
	err = d.checkDownloadedFileHash(fp)
	if err != nil {
		d.cleanAll()
		return "", err
	}
	d.log.Trace("Checked file hash")
	toolsDir, isWin, err := d.decompressDownloadedFile(fp)
	if err != nil {
		d.cleanAll()
		return toolsDir, err
	}
	d.log.Trace("Decompressed zip and extracted needed binaries")

	// chmod +x
	err = d.setExecutable(toolsDir, isWin)
	if err != nil {
		d.cleanAll()
		return toolsDir, err
	}
	d.log.Trace("Set binaries as executable")

	// all tools files downloaded, checked, archive selected and checked sha256;
	// files created and made executable
	d.cleanup()
	d.log.Trace("Cleaned up all un-needed files")
	d.log.Debugf("Tools Dir: %s", toolsDir)
	return toolsDir, nil
}

func downloadHashesFile(ctx context.Context) (string, error) {
	url := HashesLink
	toolsPath, err := getToolsBasePath()
	if err != nil {
		return "", err
	}
	err = os.MkdirAll(toolsPath, 0700)
	if err != nil {
		return "", err
	}
	hashesFilePath := filepath.Join(toolsPath, MoneroHashesFilename)
	hashesFile, err := os.Create(hashesFilePath)
	if err != nil {
		return "", err
	}
	defer hashesFile.Close()

	// HTTP GET - TODO(dl) certs
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", err
	}

	_, err = io.Copy(hashesFile, resp.Body)
	if err != nil {
		return "", err
	}
	return hashesFilePath, nil
}

func (d *Download) getHashedZips(hashesFilePath string) error {
	d.z = make(hashedZips, 0)
	hf, err := os.Open(hashesFilePath)
	if err != nil {
		return err
	}
	defer hf.Close()

	scanner := bufio.NewScanner(hf)

	gatheringHashes := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, CliTrigger) {
			gatheringHashes = true
			continue
		} else if gatheringHashes {
			if line == HashZipTerminator {
				break
			}
			tkns := strings.Split(line, TwoSpaces)
			if len(tkns) != 2 {
				return fmt.Errorf("got %d tokens, expected 2", len(tkns))
			}
			d.z = append(d.z, hashedZip{
				hash: tkns[0],
				zip:  tkns[1],
			})
		}
	}

	return scanner.Err()
}

func (d *Download) checkHashedZips() error {
	for i := range d.z {
		d.z[i].valid = true
		tkns := strings.Split(d.z[i].zip, Dash)
		if len(tkns) > 4 && len(tkns) < 3 {
			return fmt.Errorf("checking %s got %d tokens from %s, expected 3 or 4", d.z[i].zip, len(tkns), Dash)
		}
		if tkns[0] != "monero" { // all files start with monero-*
			d.z[i].valid = false
			continue
		}
		if tkns[1] == "source" || tkns[1] == "android" { // unused os's
			d.z[i].valid = false
			continue
		}
		if tkns[2] == "x86" { // no more intel 32bit arch's; sort out the arm ones then 99% done!
			d.z[i].valid = false
			continue
		}

		d.z[i].os = tkns[1]
		d.z[i].arch = tkns[2]

		verExtTkns := strings.Split(tkns[3], Dot)
		if len(verExtTkns) < 5 || len(verExtTkns) > 6 {
			return fmt.Errorf("checking %s got %d version/extension tokens, expected 5 or 6", d.z[i].zip, len(verExtTkns))
		}

		verTkns := strings.SplitN(tkns[3], Dot, 5)
		if len(verTkns) != 5 {
			return fmt.Errorf("checking %s got %d version+ tokens, expected 5", d.z[i].zip, len(verTkns))
		}
		d.z[i].ext = verTkns[4]

		mv, err := newMoneroVersionFromParts(verTkns[0], verTkns[1], verTkns[2], verTkns[3])
		if err != nil {
			return err
		}
		d.z[i].version = mv
	}
	return nil
}

func (d *Download) chooseHashedZip() error {

	// // Test Windows Hack
	// // =================
	// for i, zip := range d.z {
	// 	if zip.os == "win" && zip.arch == "x64" {
	// 		d.z[i].selected = true
	// 		d.z[i].ext = "zip"
	// 		return nil
	// 	}
	// }

	for i, zip := range d.z {
		if !zip.valid {
			continue
		}
		// similar os's - TODO(dl) some edge cases here maybe?
		if !strings.Contains(d.m.os, zip.os) {
			continue
		}
		// similar arch's - TODO(dl) some more edge cases here?
		if !strings.Contains(d.m.arch, zip.arch) && !(d.m.arch == "amd64" && zip.arch == "x64") {
			continue
		}
		d.z[i].selected = true
		return nil
	}
	return fmt.Errorf("no suitable zip found for os: %s, arch: %s", runtime.GOOS, runtime.GOARCH)
}

func (d *Download) getSelectedZip() *hashedZip {
	for _, z := range d.z {
		if z.selected {
			return &z
		}
	}
	return nil
}

func (d *Download) downloadFile(ctx context.Context) (string, error) {
	const DownloadsBase = "downloads.getmonero.org/cli"

	hzip := d.getSelectedZip()
	if hzip == nil {
		return "", fmt.Errorf("no selected zip")
	}
	url := "https://" + filepath.Join(DownloadsBase, hzip.zip)
	toolsPath, err := getToolsBasePath()
	if err != nil {
		return "", err
	}
	err = os.MkdirAll(toolsPath, 0700)
	if err != nil {
		return "", err
	}
	filePath := filepath.Join(toolsPath, hzip.zip)
	zipFile, err := os.Create(filePath)
	if err != nil {
		return "", err
	}
	defer zipFile.Close()

	// HTTP GET - TODO(dl) certs & use ctx
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", err
	}

	_, err = io.Copy(zipFile, resp.Body)
	if err != nil {
		return "", err
	}
	return filePath, nil
}

func (d *Download) checkDownloadedFileHash(filePath string) error {
	zip := d.getSelectedZip()
	if zip == nil {
		return fmt.Errorf("no selected zip")
	}
	// check sha256 hash
	zf, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer zf.Close()

	sha := sha256.New()
	if n, err := io.Copy(sha, zf); err != nil {
		return fmt.Errorf("failed to copy file content to hash: %w, written %d", err, n)
	}
	hash := hex.EncodeToString(sha.Sum(nil))

	if hash != zip.hash {
		return fmt.Errorf("bad hash %s - expected %s", hash, zip.hash)
	}
	return nil
}

func (d *Download) decompressDownloadedFile(filePath string) (string, bool, error) {
	zip := d.getSelectedZip()
	if zip == nil {
		return "", false, fmt.Errorf("no selected zip")
	}
	// decompress nix or windows compression
	if zip.ext == "tar.bz2" {
		return d.extractTarBz2(filePath)
	}
	return d.extractWinZip(filePath)
}

func (d *Download) extractWinZip(zipFilePath string) (string, bool, error) {
	// make win-style tool files dir:
	toolFilesDir := zipFilePath[:strings.LastIndex(zipFilePath, Dot)] // remove '.zip'
	err := os.MkdirAll(toolFilesDir, 0700)
	if err != nil {
		return toolFilesDir, true, fmt.Errorf("error mkdir all %s - %w", toolFilesDir, err)
	}
	zipReader, err := zip.OpenReader(zipFilePath)
	if err != nil {
		return toolFilesDir, true, fmt.Errorf("error opening zip file %s - %w", zipFilePath, err)
	}
	defer zipReader.Close()

	for _, f := range zipReader.File {
		if strings.Contains(f.Name, "monero-wallet-cli.exe") {
			rc, err := f.Open()
			if err != nil {
				return toolFilesDir, true, fmt.Errorf("error opening zip - %w", err)
			}
			cliFilePath := filepath.Join(toolFilesDir, "monero-wallet-cli.exe")
			cliFile, err := os.Create(cliFilePath)
			if err != nil {
				return toolFilesDir, true, fmt.Errorf("error creating monero-wallet-cli.exe - %w", err)
			}
			defer cliFile.Close()
			n, err := io.Copy(cliFile, rc)
			if err != nil {
				return toolFilesDir, true, fmt.Errorf("error copying monero-wallet-cli.exe - %w, written %d", err, n)
			}
			rc.Close()

		} else if strings.Contains(f.Name, "monero-wallet-rpc.exe") {
			rc, err := f.Open()
			if err != nil {
				return toolFilesDir, true, fmt.Errorf("error opening zip - %w", err)
			}
			rpcFilePath := filepath.Join(toolFilesDir, "monero-wallet-rpc.exe")
			rpcFile, err := os.Create(rpcFilePath)
			if err != nil {
				return toolFilesDir, true, fmt.Errorf("error creating monero-wallet-rpc.exe - %w", err)
			}
			defer rpcFile.Close()
			n, err := io.Copy(rpcFile, rc)
			if err != nil {
				return toolFilesDir, true, fmt.Errorf("error copying monero-wallet-rpc.exe - %w, written %d", err, n)
			}
			rc.Close()
		}
	}
	return toolFilesDir, true, nil
}

func (d *Download) extractTarBz2(filePath string) (string, bool, error) {
	zip := d.getSelectedZip()
	if zip == nil {
		return "", false, fmt.Errorf("no selected zip")
	}
	// 'tar.bz2' -> 'tar'
	var tarFilePath = filePath[:strings.LastIndex(filePath, Dot)]

	zf, err := os.Open(filePath)
	if err != nil {
		return tarFilePath, false, fmt.Errorf("failed to open file: %w", err)
	}
	defer zf.Close()

	bz2Reader := bzip2.NewReader(zf)
	tarFile, err := os.Create(tarFilePath)
	if err != nil {
		return tarFilePath, false, fmt.Errorf("failed to create output file: %w", err)
	}
	defer tarFile.Close()

	n, err := io.Copy(tarFile, bz2Reader)
	if err != nil {
		return tarFilePath, false, fmt.Errorf("failed to decompress data: %w, written %d", err, n)
	}

	d.log.Debugf("Successfully decompressed %d bytes into %s", n, tarFilePath)

	// make nix-style tool files dir:
	toolFilesDir := tarFilePath[:strings.LastIndex(tarFilePath, Dot)] // remove '.tar'
	err = os.MkdirAll(toolFilesDir, 0700)
	if err != nil {
		return toolFilesDir, false, fmt.Errorf("error mkdir all %s - %w", toolFilesDir, err)
	}
	// untar the tarfile
	tarFile, err = os.Open(tarFilePath)
	if err != nil {
		return toolFilesDir, false, fmt.Errorf("error opening tar file %s - %w", tarFilePath, err)
	}
	tarReader := tar.NewReader(tarFile)

	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break // End of the tar archive
		}
		if err != nil {
			return toolFilesDir, false, fmt.Errorf("error reading tar file %s - %w", tarFilePath, err)
		}

		if strings.Contains(hdr.Name, "monero-wallet-cli") {
			cliFilePath := filepath.Join(toolFilesDir, "monero-wallet-cli")
			cliFile, err := os.Create(cliFilePath)
			if err != nil {
				return toolFilesDir, false, fmt.Errorf("error creating new file %s - %w", cliFile.Name(), err)
			}
			defer cliFile.Close()
			_, err = io.Copy(cliFile, tarReader)
			if err != nil {
				return toolFilesDir, false, fmt.Errorf("error copying new file %s - %w", cliFile.Name(), err)
			}

		} else if strings.Contains(hdr.Name, "monero-wallet-rpc") {
			rpcFilePath := filepath.Join(toolFilesDir, "monero-wallet-rpc")
			rpcFile, err := os.Create(rpcFilePath)
			if err != nil {
				return toolFilesDir, false, fmt.Errorf("error creating new file %s - %w", rpcFile.Name(), err)
			}
			defer rpcFile.Close()
			_, err = io.Copy(rpcFile, tarReader)
			if err != nil {
				return toolFilesDir, false, fmt.Errorf("error copying new file %s - %w", rpcFile.Name(), err)
			}
		}
	}
	return toolFilesDir, false, nil
}

func (d *Download) setExecutable(toolsDir string, isWin bool) error {
	cli := filepath.Join(toolsDir, "monero-wallet-cli")
	if isWin {
		cli += ".exe" // making exe/com and read bit=1 makes executable on windows
	}
	err := os.Chmod(cli, 0755)
	if err != nil {
		return fmt.Errorf("failed to chmod %s to 0755 - error: %w", cli, err)
	}

	rpc := filepath.Join(toolsDir, "monero-wallet-rpc")
	if isWin {
		rpc += ".exe" // making exe/com and read bit=1 makes executable on windows
	}
	err = os.Chmod(rpc, 0755)
	if err != nil {
		return fmt.Errorf("failed to chmod %s to 0755 - error: %w", rpc, err)
	}
	return nil
}

func (d *Download) cleanup() {
	zip := d.getSelectedZip()
	if zip == nil {
		return
	}
	tp, err := getToolsBasePath()
	if err != nil {
		return
	}
	os.Remove(filepath.Join(tp, MoneroHashesFilename))
	os.Remove(filepath.Join(tp, zip.zip))                                   // '.tar.bz2'
	os.Remove(filepath.Join(tp, zip.zip[:strings.LastIndex(zip.zip, Dot)])) // '.tar'
}

func (d *Download) cleanAll() {
	os.RemoveAll(DexMoneroUserToolsPathDir)
}
