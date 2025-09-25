package txn

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
)

var main_tx string = "020002020010d6a2ad40aef285019a3bb8a501c1b70ce722a78701ec9f01eb01872098f001ea30e70ab11b861bb30eb165cfe5c301abbfbf4efa5eddc87a0b83" +
	"dfbb61ccc04b4ddc5c40dba28c92d4020010dcb8bd27f2808a08d4e68f11f3fb35f1950ee59c08cca802bf840fd1d203cbad019e8e06dbb304fa0c8b8b01d27a" +
	"80168472b0beaffde4233047fac63872ef116b6c0e6b72fe367ab25c1bacf22134d0020003f06910ca327b5af56964c63c47189abfb49801874864559898769b" +
	"eec44a042d580003b447e8572194d4ca7530d4c4ef0634bfe331165e725e73448a2a70d3103e8421c92c01fba9400ed36244d8f1462d4950c78ef1418f40cb9a" +
	"3ad8ec85d3fe648a673b370209019c38feb6ff54be850680fc99d302d909aa8a17e0bef465deb4e3d7ca4bc8d851405620e7e22387f95fcb7e05ee0bb7fc1bf4" +
	"04ee801b7044f54607c45059dbb1d0ad4d33ab0d09ad64b6e84c637c7de4e995cdd65326efb1415ac203ed670199a6857ab3285b025c5e3c8f0ef747d69e6078" +
	"4b4081facabdba36094e2f8b89dc3817982a0671ed520fe3dfb36e13c43294cafb7d705d6cd6ebe7b80b917613414df6507c45a3bf2197e7ec2cb7a8362de258" +
	"c4f6bf1255f82cc56b43cfe1393b9d6d8f3812b097a9df6c2f9c954dded09d9e1bc0068cd6e98f58d3372949000ab5e97e90be65612d743b8f9b7c04f5d7d56a" +
	"23754f41ac176e5f9800cf820d22b6921f2c718ecbb3449bd1dbecec83aa7becd83145d034ea349d1742bdd30207bf512816291c6ef3dd5416d79de94c394629" +
	"6aea9bc9ada85a08df50e3b8459171e44fd8d23c679fdf0488fd00c2dd348a7de48aee66600781261ed42d483a1fcf3adf9f9678d47f725c6e066ad6903b575f" +
	"411f29a3612db03dbb56539fa2f0abf139a32a025043a3624a88760cc0e962d32a21e51737392c25c8982bd37409140c0c19a2f8334c51b3edb263c7c3e6f369" +
	"e9a2390c0c33b8e51ccaf92cd4e89fd4e0445d22011f66b016093d2cb04c06d5aacbb1f427a4cee7eaa52c229079150afcb346cd3c642a8b3085b716a003e066" +
	"3e994bbaa6b54129e530d23e8a06071271173504c2f6a65bee95c31a31530885db46916260bd913197907906ff0c59a0ebd245eb401e34ab169ae643af56be88" +
	"ffb60143868557dadeb610bfcbb0768036f7430dcee5826e7d7f3e0e7ba1c054848eb62ca157e65c3cddb6d61c5b2ddc3ad5aa1faebd96995b58a87000ba5fb3" +
	"637ec05a22f44494b3eb71a581ce0bac4ff84ccdda3e25366c78cd13121be779b9d35f2c753f1d5ac5ed4aa3383968305f886fa68081d385b481463dcafe86e7" +
	"eac9f44bb78e08ad5fdcd49a1976b443436af404e11d35e94c87e9311997c2bcb4b3308362255cac7e30da9f3592164e072ca8353f06f4f28281573a75848e0e" +
	"e9cd3ec439df6791ba23f91f0b200178dcaba3f07c64d9d5e1dba861851a9bf7826d48b66f2f255893cde1a23a110cf33daaed151472bf0a1e8cd3a11161cefb" +
	"1ffde7bde845328aa069be0523e70f215053b2fd844d2ec84407c438bdc001d356805b87b7a3c4bf90066752dc5b05aa8c946d57b71ae1096f1c1611e4cdc356" +
	"9af11ecaea45457bbe649d9841400d17a8adf28c9cce91913529289c148905c1c030350e11352a5e22ef020908b401163533abe7f9d72990d8ef8b238248a01f" +
	"49e48677e11a1c2f581f37d2f5270e0bb0d684d3f044cf3b9c3cf27781fd4cc0401779da968d5edab1ca0d995fda0a0a91a8ede934af58eb864b11f1759280f7" +
	"3c237bac57c9df35cd0d21db11b3034073ade8b8a0857a55d61860c45d5078a2ef24fb11020384094ff5230a7f97034b5d60a131f8497f54dc579d1fd92556b5" +
	"e135ecb9143e0a620ee40dbe92780f4ababe3d801806543d13b8a5e3aaf14532378099e0979c0908157b3960378909edb5440eed41356e6269ee5e10d7ed9fec" +
	"118830fd1a5ef5e2c3f259807f120e85897ea2bfed5b5cbdbb532c3662163231247ad06a931c0eb4422def249dcd0a995f86e303a6c2022bd1bd4e218b321d6c" +
	"d3e534bc28dc13ed56aaa6cd4d9e087dd13b7e1ff33aef189d67fd4985fadbc08b6716e823c2e7408a264f4fa0030ccc19cd0008e8e8d66180453233ef8fc793" +
	"e184c9115aca5bb887700a93b4fa04aaa8ec5344c987dc7f220eb8d60fd40a716ec7c1925714b3013990721015b20e631301bb471c1d6bae0fbc28808b1af8c7" +
	"8673337ab6d245fa48ba6e716176074bb513ccc09d681b79f92b44da87661ec8b804ae60645e297b7081262efd6902026b123a84cc052c5b0fafe79dd3663c48" +
	"2f90b38eb647bbcbab75207b61400f509d1db376a10fb7cfae965dd86add3e14283c573ed07071158b252c6430d3070be0cccccc748c27ae9425e376767d990e" +
	"13fcd252197d2cab1951dacf0a6c01aba3ec405e86949155bb999698292973b1b5154c5ecbaa4ae97223ddeb4ed80d908050e3757d75f004e40834191fb38b42" +
	"1f31668a467a4645494260894904086a99238a8ff0d86a0d3858a4ce4e7dede7ec5a0a9b2c79ce91088dabb81d38067ca881831bab6b6acbe24e03f83315f06f" +
	"0e4424e9cac480af23a0f04196ad03d0276b72e7fc0832b4890ea415d85e52282903ab9c50794792fb67656f7e270ed6b8dfc0ebbd8fbda0de460e18a987272b" +
	"56f05483e6b56cc2645a2f4adb6f067fefe5560aeab163554f0ef2272c8c9c73107e6bfeb402232c83fc26277f3b0a85e5b25b62943253e8d220788a34fea6c6" +
	"632027a3115485aeccd3522a39e109ae333f361020d5fed519431beb6cd8c7d9d4d091eac9570f59e8e52c617fad039d1b70c4fa4fb0aad031886eb835b004df" +
	"d29add27f7e8d3659f36d1901e17082859b93522c43307fe0987e332a85ab957e92b9491e003cd6cdb117a6cd83f0ebccda03d2df196402d119ca76dea3eedc5" +
	"cdbe81f18ffb2c97ed47c251af52015dca4fa2b59bdf710498fb90731ab267369fde282198ca61d360b16db79d5c8b19837e854a6198e63ceeb0f664009d6bd5" +
	"314b1e12bbdc220f159f728035065cf0760ed4e72de10df6ab624e43429bb34c8bb8948b626a045ef5533391382ee1"

func setupParser(tx string) (*TxParser, error) {
	l := len(tx)
	if l < 1000 { // just sanity check
		return nil, fmt.Errorf("tx too short - %d", l)
	}
	p, err := NewTxParser(tx)
	if err != nil {
		return nil, fmt.Errorf("error creating parser - %v", err)
	}
	return p, nil
}

func TestParseMainTx(t *testing.T) {
	p, err := setupParser(main_tx)
	if err != nil {
		t.Fatal(err)
	}
	decodedTx, err := p.Parse()
	if err != nil {
		t.Fatal(err)
	}
	err = validateMainTx(decodedTx)
	if err != nil {
		t.Fatal(err)
	}
}

var errValidationFailed = errors.New("validation failed")

func validateMainTx(tx *XmrTx) error {
	if tx.Prefix.Version != 2 {
		return errValidationFailed
	}
	if tx.Prefix.TxUnlockTime != 0 {
		return errValidationFailed
	}
	if tx.Coinbase {
		return errValidationFailed
	}
	if tx.Prefix.Vin == nil || len(tx.Prefix.Vin) != 2 {
		return errValidationFailed
	}
	if tx.Prefix.Vin[0].Key.Amount != 0 {
		return errValidationFailed
	}
	if tx.Prefix.Vin[0].Key.KeyImage != "b165cfe5c301abbfbf4efa5eddc87a0b83dfbb61ccc04b4ddc5c40dba28c92d4" {
		return errValidationFailed
	}
	ko0 := tx.Prefix.Vin[0].Key.KeyOffsets
	if len(ko0) != 16 {
		return errValidationFailed
	}
	if ko0[0] != 134959446 || ko0[1] != 2193710 || ko0[2] != 7578 || ko0[3] != 21176 ||
		ko0[4] != 203713 || ko0[5] != 4455 || ko0[6] != 17319 || ko0[7] != 20460 || ko0[8] != 235 || ko0[9] != 4103 ||
		ko0[10] != 30744 || ko0[11] != 6250 || ko0[12] != 1383 || ko0[13] != 3505 || ko0[14] != 3462 || ko0[15] != 1843 {
		return errValidationFailed
	}

	if tx.Prefix.Vout == nil || len(tx.Prefix.Vout) != 2 {
		return errValidationFailed
	}
	if tx.Prefix.Vout[0].AmountCoinbase != 0 {
		return errValidationFailed
	}
	if tx.Prefix.Vout[0].Target.TaggedKey.StealthKey != "f06910ca327b5af56964c63c47189abfb49801874864559898769beec44a042d" {
		return errValidationFailed
	}
	if tx.Prefix.Vout[0].Target.TaggedKey.ViewTag != "58" {
		return errValidationFailed
	}

	if tx.Prefix.Vin[1].Key.Amount != 0 {
		return errValidationFailed
	}
	if tx.Prefix.Vin[1].Key.KeyImage != "8472b0beaffde4233047fac63872ef116b6c0e6b72fe367ab25c1bacf22134d0" {
		return errValidationFailed
	}
	ko1 := tx.Prefix.Vin[1].Key.KeyOffsets
	if len(ko1) != 16 {
		return errValidationFailed
	}
	if ko1[0] != 82795612 || ko1[1] != 16941170 || ko1[2] != 35910484 || ko1[3] != 884211 ||
		ko1[4] != 232177 || ko1[5] != 134757 || ko1[6] != 37964 || ko1[7] != 246335 || ko1[8] != 59729 || ko1[9] != 22219 ||
		ko1[10] != 100126 || ko1[11] != 72155 || ko1[12] != 1658 || ko1[13] != 17803 || ko1[14] != 15698 || ko1[15] != 2816 {
		return errValidationFailed
	}

	if tx.Prefix.Vout[1].AmountCoinbase != 0 {
		return errValidationFailed
	}
	if tx.Prefix.Vout[1].Target.TaggedKey.StealthKey != "b447e8572194d4ca7530d4c4ef0634bfe331165e725e73448a2a70d3103e8421" {
		return errValidationFailed
	}
	if tx.Prefix.Vout[1].Target.TaggedKey.ViewTag != "c9" {
		return errValidationFailed
	}

	if hex.EncodeToString(tx.Extra) != "01fba9400ed36244d8f1462d4950c78ef1418f40cb9a3ad8ec85d3fe648a673b370209019c38feb6ff54be85" {
		return errValidationFailed
	}

	if tx.ExtraTxKey != "fba9400ed36244d8f1462d4950c78ef1418f40cb9a3ad8ec85d3fe648a673b37" {
		return errValidationFailed
	}

	sa := tx.GetStealthAddresses()
	if sa == nil || len(sa) != 2 {
		return errValidationFailed
	}
	if sa[0] != "f06910ca327b5af56964c63c47189abfb49801874864559898769beec44a042d" || sa[1] != "b447e8572194d4ca7530d4c4ef0634bfe331165e725e73448a2a70d3103e8421" {
		return errValidationFailed
	}
	return nil
}

type varintTestData struct {
	stream   []byte
	expected uint64
}

var varintTests = []varintTestData{
	// varints generated from Monero test code tested against go Uvarint
	{stream: []byte{0x00}, expected: 0},
	{stream: []byte{0x01}, expected: 1},
	{stream: []byte{0x7f}, expected: 127},
	{stream: []byte{0x80, 0x01}, expected: 128},
	{stream: []byte{0x81, 0x01}, expected: 129},
	{stream: []byte{0xff, 0x03}, expected: 511},
	{stream: []byte{0x80, 0x04}, expected: 512},
	{stream: []byte{0xff, 0x07}, expected: 1023},
	{stream: []byte{0x80, 0x08}, expected: 1024},
	{stream: []byte{0x9a, 0x3b}, expected: 7578},
	{stream: []byte{0xa7, 0x87, 0x01}, expected: 17319},
	{stream: []byte{0xec, 0x9f, 0x01}, expected: 20460},
	{stream: []byte{0xff, 0xff, 0x01}, expected: 32767},
	{stream: []byte{0x80, 0x80, 0x02}, expected: 32768},
	{stream: []byte{0xfe, 0xff, 0x03}, expected: 65534},
	{stream: []byte{0xff, 0xff, 0x03}, expected: 65535},
	{stream: []byte{0x80, 0x80, 0x04}, expected: 65536},
	// Monero test from 0 .. 65536 only .. these are from a random tx.
	{stream: []byte{0xc1, 0xb7, 0x0c}, expected: 203713},
	{stream: []byte{0xae, 0xf2, 0x85, 0x01}, expected: 2193710},
	{stream: []byte{0xd6, 0xa2, 0xad, 0x40}, expected: 134959446},
}

func TestReadVarint(t *testing.T) {
	for i, test := range varintTests {
		rdr := bytes.NewReader(test.stream)
		n, err := binary.ReadUvarint(rdr)
		if err != nil {
			t.Fatalf("test %d %v", i, err)
		}
		if n != test.expected {
			t.Fatalf("test %d %v", i, err)
			continue
		}
	}
}
