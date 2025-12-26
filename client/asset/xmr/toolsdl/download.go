package toolsdl

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/bzip2"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/dexnet"
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
// from where they can be subsequently run.

// This is the relevant part of hashes.txt:
//
// ## CLI
// 7c2ad18ca3a1ad5bc603630ca935a753537a38a803e98d645edd6a3b94a5f036  monero-android-armv7-v0.18.4.4.tar.bz2
// eb81b71f029884ab5fec76597be583982c95fd7dc3fc5f5083a422669cee311e  monero-android-armv8-v0.18.4.4.tar.bz2
// bc539178df23d1ae8b69569d9c328b5438ae585c0aacbebe12d8e7d387a745b0  monero-freebsd-x64-v0.18.4.4.tar.bz2
// 2040dc22748ef39ed8a755324d2515261b65315c67b91f449fa1617c5978910b  monero-linux-armv7-v0.18.4.4.tar.bz2
// b9daede195a24bdd05bba68cb5cb21e42c2e18b82d4d134850408078a44231c5  monero-linux-armv8-v0.18.4.4.tar.bz2
// c939ea6e8002798f24a56ac03cbfc4ff586f70d7d9c3321b7794b3bcd1fa4c45  monero-linux-riscv64-v0.18.4.4.tar.bz2
// 7fe45ee9aade429ccdcfcad93b905ba45da5d3b46d2dc8c6d5afc48bd9e7f108  monero-linux-x64-v0.18.4.4.tar.bz2
// 8c174b756e104534f3d3a69fe68af66d6dc4d66afa97dfe31735f8d069d20570  monero-linux-x86-v0.18.4.4.tar.bz2
// 645e9bbae0275f555b2d72a9aa30d5f382df787ca9528d531521750ce2da9768  monero-mac-armv8-v0.18.4.4.tar.bz2
// af3d98f09da94632db3e2f53c62cc612e70bf94aa5942d2a5200b4393cd9c842  monero-mac-x64-v0.18.4.4.tar.bz2
// 7eb3b87a105b3711361dd2b3e492ad14219d21ed8fd3dd726573a6cbd96e83a6  monero-win-x64-v0.18.4.4.zip
// a148a2bd2b14183fb36e2cf917fce6f33fb687564db2ed53193b8432097ab398  monero-win-x86-v0.18.4.4.zip
// 84570eee26238d8f686605b5e31d59569488a3406f32e7045852de91f35508a2  monero-source-v0.18.4.4.tar.bz2
// #
//
// A hashedZip struct (below) represents one line '[sha hash] [monero-<os>-<arch>-<version>.<extension>]'
// from the above snippet.

const (
	HashesLink     = "https://www.getmonero.org/downloads/hashes.txt"
	HashesFilename = "hashes.txt"
	ShareDir       = "share"
	MoneroToolsDir = "monero-tools"
)

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
	CliTrigger           = "## CLI"
	TwoSpaces            = "  "
	Dash                 = "-"
	Dot                  = "."
	HashedZipsTerminator = "#"
)

const (
	WinZipExt = ".zip"
	TarBz2Ext = ".tar.bz2"
)

// See Also: relevant part of hashes.txt above.
type hashedZip struct {
	hash     string
	zip      string
	dir      string
	ext      string // zip or tar.bz2
	os       string
	arch     string
	valid    bool
	selected bool
	version  *moneroVersionV0
}

type hashedZips []hashedZip

type Download struct {
	DataDir     string
	Log         dex.Logger
	machine     *machine
	tempDir     string
	hzips       hashedZips
	selectedZip *hashedZip
}

func (d *Download) getSelectedZip() *hashedZip {
	return d.selectedZip
}

// getToolsBasePath gets --appdir/defaultApplicationDirectory back from DataDir
// since we only have DataDir. Then we add the required additional  directories
// for tools base.
//
// This relies on (*Core),assetDataDirectory having created the specific extra
// three directories which we work back from so there are no validity checks.
func (d *Download) getToolsBasePath() string {
	dataDirPath := d.DataDir
	assetdbDirPath := filepath.Dir(dataDirPath)
	netDirPath := filepath.Dir(assetdbDirPath)
	appDirPath := filepath.Dir(netDirPath)
	return filepath.Join(appDirPath, ShareDir, MoneroToolsDir)
}

// getToolsTempPath gets base path relative to a temp dir created on Run
func (d *Download) getToolsTempPath() string {
	return filepath.Join(d.tempDir, ShareDir, MoneroToolsDir)
}

// ============================== local ======================================
// GetBestCurrentLocalToolsDir retrieves latest local version directory from ToolsPath.
// It checks any valid dir returned by getAllLocalToolsDirs to determine the latest
// tools dir if any.
//
// Return: if one or more paths exist, full path, dir. Any error returned will be
// from reading dirs and paths.
func (d *Download) GetBestCurrentLocalToolsDir() (bool, string, string, error) {
	dirs, err := d.getAllLocalToolsDirs()
	if err != nil {
		return false, "", "", err
	}
	numDirs := len(dirs)

	if numDirs == 0 {
		return false, "", "", nil
	}

	if numDirs == 1 {
		return true, filepath.Join(d.getToolsBasePath(), dirs[0].Name()), dirs[0].Name(), nil
	}

	// more than one tools dir

	// find the one dir that is most up to date
	type latestVer struct {
		mv      *moneroVersionV0
		dirname string
	}
	latest := &latestVer{
		mv:      moneroVersionZeroV0(), // v0.0.0.0
		dirname: "",
	}

	laterVersionFound := false
	for _, dentry := range dirs {
		mv, err := newMoneroVersionFromDir(dentry.Name())
		if err != nil {
			d.Log.Warnf("bad dir name: %s seen in GetBestCurrentLocalToolsDir", dentry.Name())
			continue
		}
		rc := mv.compare(latest.mv)
		if rc >= 0 {
			// this entry version is >= latest looked at
			latest.dirname = dentry.Name()
			latest.mv = mv
			laterVersionFound = true
		}
	}
	if laterVersionFound {
		return true, filepath.Join(d.getToolsBasePath(), latest.dirname), latest.dirname, nil
	}

	return false, "", "", nil
}

func (d *Download) getAllLocalToolsDirs() ([]os.DirEntry, error) {
	toolsPath := d.getToolsBasePath()
	// does toolsPath exist
	_, err := os.Stat(toolsPath)
	if err != nil {
		return nil, nil // not an error
	}
	entries, err := os.ReadDir(toolsPath)
	if err != nil {
		return nil, fmt.Errorf("error reading tools dir - %w", err)
	}

	var dirs []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "monero") {
			hasBins, err := hasMoneroBinaries(filepath.Join(toolsPath, entry.Name()))
			if err != nil {
				return nil, err
			}
			if hasBins {
				dirs = append(dirs, entry)
			}
		}
	}
	return dirs, nil
}

func hasMoneroBinaries(dirPath string) (bool, error) {
	var gotCli, gotRpc bool
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "monero-wallet-cli", "monero-wallet-cli.exe":
			gotCli = true
		case "monero-wallet-rpc", "monero-wallet-rpc.exe":
			gotRpc = true
		}
	}
	return gotCli && gotRpc, nil
}

// ============================== remote ======================================

var ErrNoRemoteVersion = errors.New("cannot find appropriate tools version in hashes.txt")

// downloadRemoteCanonicalHashesFile downloads hashes.txt from getmonero.org and stores it's
// local filepath.
func (d *Download) downloadRemoteCanonicalHashesFile() {
	hashesFilePath, err := d.downloadHashesFile()
	if err != nil {
		return
	}
	d.Log.Trace("Hashes file downloaded")
	err = d.getHashedZips(hashesFilePath)
	if err != nil {
		return
	}
	d.Log.Trace("Collected hashed zips")
	if len(d.hzips) <= 0 {
		return
	}
	err = d.checkHashedZips()
	if err != nil {
		return
	}
	d.Log.Trace("Checked hashed zips")
	err = d.chooseHashedZip()
	if err != nil {
		return
	}
}

func (d *Download) downloadHashesFile() (string, error) {
	url := HashesLink
	toolsPath := d.getToolsTempPath()
	err := os.MkdirAll(toolsPath, 0700)
	if err != nil {
		return "", err
	}
	hashesFilePath := filepath.Join(toolsPath, HashesFilename)
	hashesFile, err := os.Create(hashesFilePath)
	if err != nil {
		return "", err
	}
	defer hashesFile.Close()

	resp, err := dexnet.Client.Get(url)
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
	d.hzips = make(hashedZips, 0)
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
			if line == HashedZipsTerminator {
				break
			}
			tkns := strings.Split(line, TwoSpaces)
			if len(tkns) != 2 {
				return fmt.Errorf("got %d tokens, expected 2", len(tkns))
			}
			d.hzips = append(d.hzips, hashedZip{
				hash: tkns[0],
				zip:  tkns[1],
				dir:  getDirFromZip(tkns[1]),
			})
		}
	}

	return scanner.Err()
}

func getDirFromZip(zipTkn string) string {
	isWinZip := strings.HasSuffix(zipTkn, WinZipExt)
	isTarBz2 := strings.HasSuffix(zipTkn, TarBz2Ext)
	if !isWinZip && !isTarBz2 {
		return zipTkn
	}
	var lastIndex int
	if isWinZip {
		lastIndex = strings.LastIndex(zipTkn, WinZipExt)
	}
	if isTarBz2 {
		lastIndex = strings.LastIndex(zipTkn, TarBz2Ext)
	}
	return zipTkn[:lastIndex]
}

func (d *Download) checkHashedZips() error {
	for i := range d.hzips {
		d.hzips[i].valid = true
		tkns := strings.Split(d.hzips[i].zip, Dash)
		if len(tkns) > 4 || len(tkns) < 3 {
			return fmt.Errorf("checking %s got %d tokens from %s, expected 3 or 4", d.hzips[i].zip, len(tkns), Dash)
		}
		if tkns[0] != "monero" { // all files start with monero-*
			d.hzips[i].valid = false
			continue
		}
		if tkns[1] == "source" || tkns[1] == "android" { // unused tokens
			d.hzips[i].valid = false
			continue
		}
		if tkns[2] == "x86" { // no more intel 32bit arch's
			d.hzips[i].valid = false
			continue
		}

		// check we have cut out the 3-token possibility above (source, which has no arch) from the valid set.
		if len(tkns) != 4 {
			return fmt.Errorf("incorrect number of tokens %d, expected 4", len(tkns))
		}

		d.hzips[i].os = tkns[1]
		d.hzips[i].arch = tkns[2]

		verExtTkns := strings.Split(tkns[3], Dot)
		if len(verExtTkns) < 5 || len(verExtTkns) > 6 {
			return fmt.Errorf("checking %s got %d version/extension tokens, expected 5 or 6", d.hzips[i].zip, len(verExtTkns))
		}

		verTkns := strings.SplitN(tkns[3], Dot, 5)
		if len(verTkns) != 5 {
			return fmt.Errorf("checking %s got %d version+ tokens, expected 5", d.hzips[i].zip, len(verTkns))
		}
		d.hzips[i].ext = verTkns[4]

		mv, err := newMoneroVersionFromParts(verTkns[0], verTkns[1], verTkns[2], verTkns[3])
		if err != nil {
			return err
		}
		if !mv.valid() {
			return fmt.Errorf("invalid monero version for zip %s", mv.string())
		}
		d.hzips[i].version = mv
	}
	return nil
}

// chooseHashedZip maps a runtime.GOOS/GOARCH for this machine onto monero zip
// dirname.
func (d *Download) chooseHashedZip() error {
	for i, zip := range d.hzips {
		if !zip.valid {
			continue
		}
		// similar os's
		if !(d.machine.os == zip.os) &&
			!(d.machine.os == "darwin" && zip.os == "mac") && !(d.machine.os == "windows" && zip.os == "win") {
			continue
		}
		// similar arch's
		if !(d.machine.arch == zip.arch) &&
			!(d.machine.arch == "amd64" && zip.arch == "x64") && !(d.machine.arch == "arm64" && zip.arch == "armv8") {
			continue
		}
		d.Log.Tracef("Chose zip for machine: %s %s", d.machine.os, d.machine.arch)
		d.hzips[i].selected = true // maybe remove
		d.selectedZip = &d.hzips[i]
		return nil
	}
	return fmt.Errorf("no suitable zip found for os: %s, arch: %s", d.machine.os, d.machine.arch)
}

// ================================== Run ======================================
// Run downloads a new set of dex monero tools of the latest version if it does
// not already exist on the filesystem. If there are no dex monero tools already
// and download also fails then we download of one of our JSON version sets.
// =============================================================================
func (d *Download) Run() (string, error) {
	noLocal := false
	noZip := false

	d.machine = getMachine()

	// working dir
	tempDir, err := os.MkdirTemp("", "share-mtools-") // unix: /tmp/share-mtools-1234567
	if err != nil {
		noZip = true
	}
	d.tempDir = tempDir

	// what do we have locally
	hasPath, bestLocalToolsDir, bestLocalDirName, err := d.GetBestCurrentLocalToolsDir()
	if err != nil || !hasPath {
		noLocal = true
	}
	localVer, err := newMoneroVersionFromDir(bestLocalDirName)
	if err != nil {
		noLocal = true
	}

	// what do we have remotely
	d.downloadRemoteCanonicalHashesFile()
	hzip := d.getSelectedZip()
	if hzip == nil {
		noZip = true
	}

	// 1. only hard coded
	if noLocal && noZip {
		return d.runMavDownload()
	}

	// 2. canon first, hard coded second
	if noLocal && !noZip {
		toolsDir, err := d.runDownload()
		if err != nil {
			return d.runMavDownload()
		}
		return toolsDir, nil
	}

	// 3. local unless hard coded higher version
	if !noLocal && noZip {
		hcmvs, _ := getMoneroMAVersionSet()
		var hcVer = moneroVersionZeroV0()
		for _, hcmv := range hcmvs {
			// hard coded is greater than local version.
			// This cannot happen yet from the hard coded json
			if hcmv.compare(localVer) > 0 {
				hcVer = hcmv
			}
		}
		if hcVer.compare(localVer) <= 0 { // local as good or better than hard coded
			return bestLocalToolsDir, nil
		}
		mavToolsDir, err := d.runMavDownload()
		if err != nil {
			return bestLocalToolsDir, nil
		}
		return mavToolsDir, nil
	}

	// 4. got a best local, a valid hashedZip set.
	if !noLocal && !noZip {
		remVer := hzip.version
		if remVer.compare(localVer) > 0 {
			toolsDir, err := d.runDownload()
			if err != nil {
				return bestLocalToolsDir, nil
			}
			return toolsDir, nil
		}
		return bestLocalToolsDir, nil
	}

	return "", nil
}

func (d *Download) runMavDownload() (string, error) {
	vset, err := getOtherAcceptableVersions(d.getToolsBasePath())
	if err != nil {
		return "", fmt.Errorf("error getting other acceptable versions - %w", err)
	}
	if vset == nil {
		return "", fmt.Errorf("no other acceptable versions")
	}

	// descending versions
	slices.SortFunc(vset.versions, func(this, other Version) int {
		mvThis, _ := newMoneroVersionFromDir(this.AcceptableZips[0].Dir)
		mvOther, _ := newMoneroVersionFromDir(other.AcceptableZips[0].Dir)
		return mvThis.compare(mvOther)
	})

	var toolsDir string

	// try all versions from highest to lowest. first hit attempts a download.
	// if error then try lower acceptable versions.
	for _, v := range vset.versions {
		hzips := v.getHashedZips()
		// hzips should all have same version
		for i, az := range v.AcceptableZips {
			mv, err := newMoneroVersionFromDir(az.Dir)
			if err != nil {
				return "", fmt.Errorf("mav: bad acceptableVersion monero version from Dir: %s", az.Dir)
			}
			if mv.compare(hzips[i].version) != 0 {
				return "", fmt.Errorf("mav: acceptableVersion version %s is not the same as hashedZip version %s", az.Dir, hzips[i].version.string())
			}
		}

		d.hzips = hzips

		err := d.chooseHashedZip()
		if err != nil {
			d.Log.Warnf("mav: error choosing hashed zip for os/arch: %s/%s - %w", d.machine.os, d.machine.arch, err)
			continue
		}
		// run this download
		toolsDir, err = d.runDownload()
		if err != nil {
			currVer, _ := newMoneroVersionFromDir(v.AcceptableZips[0].Dir)
			d.Log.Warnf("mav: error downloading and installing chosen hashed zip version %s for os/arch: %s/%s - %w",
				currVer.string(), d.machine.os, d.machine.arch, err)
			continue
		}
		return toolsDir, nil
	}

	return "", fmt.Errorf("no acceptable zip was downloaded")
}

// runDownload performs a full download and check of the latest canonical version of the
// monero-wallet-cli & monero-wallet-rpc tools and installs in ToolsDir if valid.
// Returns ToolsDir on success.
func (d *Download) runDownload() (string, error) {
	// zip already selected
	fp, err := d.downloadFile()
	if err != nil {
		return "", err
	}
	d.Log.Trace("Downloaded zip file")
	err = d.checkDownloadedFileHash(fp)
	if err != nil {
		return "", err
	}
	d.Log.Trace("Checked file hash, ok!")
	d.Log.Trace("Starting to decompress zip ...")
	toolsDir, isWin, err := d.decompressDownloadedFile(fp)
	if err != nil {
		return toolsDir, err
	}
	d.Log.Trace("Decompressed zip and extracted needed binaries")

	// chmod +x
	err = d.setExecutable(toolsDir, isWin)
	if err != nil {
		return toolsDir, err
	}
	d.Log.Trace("Set binaries as executable")

	// all tools files downloaded, checked, archive selected and checked sha256;
	// files created and made executable. Copy across tools to the real dir after
	// creating a new versioned tools dir.
	newToolsDir, err := d.copyNewFiles()
	if err != nil {
		return "", err
	}
	d.Log.Debugf("Tools Dir: %s", newToolsDir)
	return newToolsDir, nil
}

func (d *Download) downloadFile() (string, error) {
	const DownloadsBase = "downloads.getmonero.org/cli"

	hzip := d.getSelectedZip()
	if hzip == nil {
		return "", fmt.Errorf("no selected zip")
	}
	url := "https://" + filepath.Join(DownloadsBase, hzip.zip)
	toolsPath := d.getToolsTempPath()
	err := os.MkdirAll(toolsPath, 0700)
	if err != nil {
		return "", err
	}
	filePath := filepath.Join(toolsPath, hzip.zip)
	zipFile, err := os.Create(filePath)
	if err != nil {
		return "", err
	}
	defer zipFile.Close()

	resp, err := dexnet.Client.Get(url)
	// resp, err := http.Get(url)
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

	d.Log.Debugf("Successfully decompressed %d bytes into %s", n, tarFilePath)

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

func (d *Download) copyNewFiles() (string, error) {
	zip := d.getSelectedZip()
	if zip == nil {
		return "", fmt.Errorf("copy new files - cannot get selected zip")
	}
	// new
	newDir := filepath.Join(d.getToolsBasePath(), zip.dir)
	err := os.MkdirAll(newDir, 0700)
	if err != nil {
		return "", fmt.Errorf("cannot make new dir on tools base path - %w", err)
	}
	newDirCliFile := filepath.Join(newDir, "monero-wallet-cli")
	if d.machine.os == "windows" {
		newDirCliFile += ".exe"
	}
	newDirRpcFile := filepath.Join(newDir, "monero-wallet-rpc")
	if d.machine.os == "windows" {
		newDirCliFile += ".exe"
	}
	// temp
	tempDir := filepath.Join(d.getToolsTempPath(), zip.dir)
	tempDirCliFile := filepath.Join(tempDir, "monero-wallet-cli")
	if d.machine.os == "windows" {
		tempDirCliFile += ".exe"
	}
	tempDirRpcFile := filepath.Join(tempDir, "monero-wallet-rpc")
	if d.machine.os == "windows" {
		tempDirCliFile += ".exe"
	}
	// move
	err = os.Rename(tempDirCliFile, newDirCliFile)
	if err != nil {
		return "", fmt.Errorf("cannot move temp cli to new dir cli - %w", err)
	}
	err = os.Rename(tempDirRpcFile, newDirRpcFile)
	if err != nil {
		return "", fmt.Errorf("cannot move temp rpc to new dir rpc - %w", err)
	}

	return newDir, nil
}
