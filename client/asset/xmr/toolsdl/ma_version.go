package toolsdl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ============================================================================
//  Minimum Acceptable Version
//
// This file contains:
// - hard coded json definition for minimum version (which can be extended)
// - hooks to allow an overriding set of json version defs in a file to be used
// ============================================================================

const (
	VersionsFilename = "versions.json"
)

func init() {
	minimumVersion, _ = newMoneroVersionFromParts("v0", "18", "4", "3") // minimum for any set of hashed zips
}

// There is only one signed hashes.txt in existence on getmonero.org i.e. the latest.
// But these are the validated contents of hashes.txt for v0.18.4.3. This information
// comes from https://github.com/monero-project/monero/releases/tag/v0.18.4.3
//
// monero-win-x64-v0.18.4.3.zip, bd9f615657c35d2d7dd9a5168ad54f1547dbf9a335dee7f12fab115f6f394e36
// monero-win-x86-v0.18.4.3.zip, e642ed7bbfa34c30b185387fa553aa9c3ea608db1f3fc0e9332afa9b522c9c1a
// monero-mac-x64-v0.18.4.3.tar.bz2, a8d8273b14f31569f5b7aa3063fbd322e3caec3d63f9f51e287dfc539c7f7d61
// monero-mac-armv8-v0.18.4.3.tar.bz2, bab9a6d3c2ca519386cff5ff0b5601642a495ed1a209736acaf354468cba1145
// monero-linux-x64-v0.18.4.3.tar.bz2, 3a7b36ae4da831a4e9913e0a891728f4c43cd320f9b136cdb6686b1d0a33fafa
// monero-linux-x86-v0.18.4.3.tar.bz2, e0b51ca71934c33cb83cfa8535ffffebf431a2fc9efe3acf2baad96fb6ce21ec
// monero-linux-armv8-v0.18.4.3.tar.bz2, b1cc5f135de3ba8512d56deb4b536b38c41addde922b2a53bf443aeaf2a5a800
// monero-linux-armv7-v0.18.4.3.tar.bz2, 3ac83049bc565fb5238501f0fa629cdd473bbe94d5fb815088af8e6ff1d761cd
// monero-linux-riscv64-v0.18.4.3.tar.bz2 95baaa6e8957b92caeaed7fb19b5c2659373df8dd5f4de2601ed3dae7b17ce2f
// monero-android-armv8-v0.18.4.3.tar.bz2, 1aebd24aaaec3d1e87a64163f2e30ab2cd45f3902a7a859413f6870944775c21
// monero-android-armv7-v0.18.4.3.tar.bz2, 4e1481835824b9233f204553d4a19645274824f3f6185d8a4b50198470752f54
// monero-freebsd-x64-v0.18.4.3.tar.bz2, ff7b9c5cf2cb3d602c3dff1902ac0bc3394768cefc260b6003a9ad4bcfb7c6a4

var jsonVersions = `
[
	{
		"zips":[
			{
				"hash":"bd9f615657c35d2d7dd9a5168ad54f1547dbf9a335dee7f12fab115f6f394e36",
				"zip":"monero-win-x64-v0.18.4.3.zip",
				"dir":"monero-win-x64-v0.18.4.3",
				"ext":"zip",
				"os":"win",
				"arch":"x64"
			},
			{
				"hash":"a8d8273b14f31569f5b7aa3063fbd322e3caec3d63f9f51e287dfc539c7f7d61",
				"zip":"monero-mac-x64-v0.18.4.3.tar.bz2",
				"dir":"monero-mac-x64-v0.18.4.3",
				"ext":"tar.bz2",
				"os":"mac",
				"arch":"x64"
			},		{
				"hash":"bab9a6d3c2ca519386cff5ff0b5601642a495ed1a209736acaf354468cba1145",
				"zip":"monero-mac-armv8-v0.18.4.3.tar.bz2",
				"dir":"monero-mac-armv8-v0.18.4.3",
				"ext":"tar.bz2",
				"os":"mac",
				"arch":"armv8"
			},
			{
				"hash":"3a7b36ae4da831a4e9913e0a891728f4c43cd320f9b136cdb6686b1d0a33fafa",
				"zip":"monero-linux-x64-v0.18.4.3.tar.bz2",
				"dir":"monero-linux-x64-v0.18.4.3",
				"ext":"tar.bz2",
				"os":"linux",
				"arch":"x64"
			},
			{
				"hash":"b1cc5f135de3ba8512d56deb4b536b38c41addde922b2a53bf443aeaf2a5a800",
				"zip":"monero-linux-armv8-v0.18.4.3.tar.bz2",
				"dir":"monero-linux-armv8-v0.18.4.3",
				"ext":"tar.bz2",
				"os":"linux",
				"arch":"armv8"
			},
			{
				"hash":"95baaa6e8957b92caeaed7fb19b5c2659373df8dd5f4de2601ed3dae7b17ce2f",
				"zip":"monero-linux-riscv64-v0.18.4.3.tar.bz2",
				"dir":"monero-linux-riscv64-v0.18.4.3",
				"ext":"tar.bz2",
				"os":"linux",
				"arch":"riscv64"
			},
			{
				"hash":"ff7b9c5cf2cb3d602c3dff1902ac0bc3394768cefc260b6003a9ad4bcfb7c6a4",
				"zip":"monero-freebsd-x64-v0.18.4.3.tar.bz2",
				"dir":"monero-freebsd-x64-v0.18.4.3",
				"ext":"tar.bz2",
				"os":"freebsd",
				"arch":"x64"
			}
		]
	}
]
`

type AcceptableZip struct {
	Hash string `json:"hash"`
	Zip  string `json:"zip"`
	Dir  string `json:"dir"`
	Ext  string `json:"ext"`
	Os   string `json:"os"`
	Arch string `json:"arch"`
}

type AcceptableZipsList []AcceptableZip

type Version struct {
	AcceptableZips AcceptableZipsList `json:"zips"`
}

type VersionsList []Version

type VersionSet struct {
	Versions VersionsList `json:"versions"` // no tag
}

type moneroVersionSet []*moneroVersionV0

func (v *Version) getHashedZips() hashedZips {
	lenAcceptableZips := len(v.AcceptableZips)
	var hzips = make(hashedZips, lenAcceptableZips)
	for i, acceptableZip := range v.AcceptableZips {
		mv, err := newMoneroVersionFromDir(acceptableZip.Dir)
		if err != nil {
			continue
		}
		hzip := hashedZip{
			hash:    acceptableZip.Hash,
			zip:     acceptableZip.Zip,
			dir:     acceptableZip.Dir,
			ext:     acceptableZip.Ext,
			os:      acceptableZip.Os,
			arch:    acceptableZip.Arch,
			valid:   true,
			version: mv,
		}
		hzips[i] = hzip
	}
	return hzips
}

// getOtherAcceptableVersions gets other acceptable Zip versions from 'versions.json'
// if it exists or from json definition(s) above. JSON file contents override any
// hard-coded information in this file.
func getOtherAcceptableVersions(toolsDir string) (*VersionSet, error) {
	vsFile, err := getVersionsFromJsonFile(toolsDir)
	if err == nil {
		return vsFile, nil
	}
	// json file not gettable, corrupt or likely missing
	vsJson, err := getHardCodedJsonVersions()
	if err != nil {
		return nil, err
	}
	return vsJson, nil
}

func getVersionsFromJsonFile(toolsDir string) (*VersionSet, error) {
	var vs VersionSet
	versionsFilepath := filepath.Join(toolsDir, VersionsFilename)
	versions, err := os.ReadFile(versionsFilepath)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(versions, &vs.Versions)
	if err != nil {
		fmt.Printf("unmarshall error - %v\n", err)
	}
	return &vs, nil
}

func getHardCodedJsonVersions() (*VersionSet, error) {
	var vs VersionSet
	err := json.Unmarshal([]byte(jsonVersions), &vs.Versions)
	if err != nil {
		return nil, fmt.Errorf("unmarshall all versions - %w", err)
	}
	return &vs, nil
}

func getMoneroMAVersionSet() (moneroVersionSet, error) {
	var mvs moneroVersionSet
	vs, err := getHardCodedJsonVersions()
	if err != nil {
		return nil, err
	}
	lenVersions := len(vs.Versions)
	mvs = make(moneroVersionSet, lenVersions)
	for i, v := range vs.Versions {
		mv, err := newMoneroVersionFromDir(v.AcceptableZips[0].Dir)
		if err != nil {
			continue
		}
		mvs[i] = mv
	}
	return mvs, nil
}
