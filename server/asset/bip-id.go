// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package asset

// BipIDs maps ticker symbols to a unique ID based on registered BIP-0044 coin
// indices.
var BipIDs = map[string]uint32{
	"BTC":         0,        // Bitcoin
	"Testnet":     1,        // (all coins)
	"LTC":         2,        // Litecoin
	"DOGE":        3,        // Dogecoin
	"RDD":         4,        // Reddcoin
	"DASH":        5,        // Dash (ex Darkcoin)
	"PPC":         6,        // Peercoin
	"NMC":         7,        // Namecoin
	"FTC":         8,        // Feathercoin
	"XCP":         9,        // Counterparty
	"BLK":         10,       // Blackcoin
	"NSR":         11,       // NuShares
	"NBT":         12,       // NuBits
	"MZC":         13,       // Mazacoin
	"VIA":         14,       // Viacoin
	"XCH":         15,       // ClearingHouse
	"RBY":         16,       // Rubycoin
	"GRS":         17,       // Groestlcoin
	"DGC":         18,       // Digitalcoin
	"CCN":         19,       // Cannacoin
	"DGB":         20,       // DigiByte
	"Open":        21,       // Assets
	"MONA":        22,       // Monacoin
	"CLAM":        23,       // Clams
	"XPM":         24,       // Primecoin
	"NEOS":        25,       // Neoscoin
	"JBS":         26,       // Jumbucks
	"ZRC":         27,       // ziftrCOIN
	"VTC":         28,       // Vertcoin
	"NXT":         29,       // NXT
	"BURST":       30,       // Burst
	"MUE":         31,       // MonetaryUnit
	"ZOOM":        32,       // Zoom
	"VASH":        33,       // Virtual Cash Also known as VPNcoin
	"CDN":         34,       // Canada eCoin
	"SDC":         35,       // ShadowCash
	"PKB":         36,       // ParkByte
	"PND":         37,       // Pandacoin
	"START":       38,       // StartCOIN
	"MOIN":        39,       // MOIN
	"EXP":         40,       // Expanse
	"EMC2":        41,       // Einsteinium
	"DCR":         42,       // Decred
	"XEM":         43,       // NEM
	"PART":        44,       // Particl
	"ARG":         45,       // Argentum
	"Libertas":    46,       //
	"Posw":        47,       // coin
	"SHR":         48,       // Shreeji
	"GCR":         49,       // Global Currency Reserve (GCRcoin)
	"NVC":         50,       // Novacoin
	"AC":          51,       // Asiacoin
	"BTCD":        52,       // Bitcoindark
	"DOPE":        53,       // Dopecoin
	"TPC":         54,       // Templecoin
	"AIB":         55,       // AIB
	"EDRC":        56,       // EDRCoin
	"SYS":         57,       // Syscoin
	"SLR":         58,       // Solarcoin
	"SMLY":        59,       // Smileycoin
	"ETH":         60,       // Ether
	"ETC":         61,       // Ether Classic
	"PSB":         62,       // Pesobit
	"LDCN":        63,       // Landcoin
	"Open":        64,       // Chain
	"XBC":         65,       // Bitcoinplus
	"IOP":         66,       // Internet of People
	"NXS":         67,       // Nexus
	"INSN":        68,       // InsaneCoin
	"OK":          69,       // OKCash
	"BRIT":        70,       // BritCoin
	"CMP":         71,       // Compcoin
	"CRW":         72,       // Crown
	"BELA":        73,       // BelaCoin
	"ICX":         74,       // ICON
	"FJC":         75,       // FujiCoin
	"MIX":         76,       // MIX
	"XVG":         77,       // Verge
	"EFL":         78,       // Electronic Gulden
	"CLUB":        79,       // ClubCoin
	"RICHX":       80,       // RichCoin
	"POT":         81,       // Potcoin
	"QRK":         82,       // Quarkcoin
	"TRC":         83,       // Terracoin
	"GRC":         84,       // Gridcoin
	"AUR":         85,       // Auroracoin
	"IXC":         86,       // IXCoin
	"NLG":         87,       // Gulden
	"BITB":        88,       // BitBean
	"BTA":         89,       // Bata
	"XMY":         90,       // Myriadcoin
	"BSD":         91,       // BitSend
	"UNO":         92,       // Unobtanium
	"MTR":         93,       // MasterTrader
	"GB":          94,       // GoldBlocks
	"SHM":         95,       // Saham
	"CRX":         96,       // Chronos
	"BIQ":         97,       // Ubiquoin
	"EVO":         98,       // Evotion
	"STO":         99,       // SaveTheOcean
	"BIGUP":       100,      // BigUp
	"GAME":        101,      // GameCredits
	"DLC":         102,      // Dollarcoins
	"ZYD":         103,      // Zayedcoin
	"DBIC":        104,      // Dubaicoin
	"STRAT":       105,      // Stratis
	"SH":          106,      // Shilling
	"MARS":        107,      // MarsCoin
	"UBQ":         108,      // Ubiq
	"PTC":         109,      // Pesetacoin
	"NRO":         110,      // Neurocoin
	"ARK":         111,      // ARK
	"USC":         112,      // UltimateSecureCashMain
	"THC":         113,      // Hempcoin
	"LINX":        114,      // Linx
	"ECN":         115,      // Ecoin
	"DNR":         116,      // Denarius
	"PINK":        117,      // Pinkcoin
	"ATOM":        118,      // Atom
	"PIVX":        119,      // Pivx
	"FLASH":       120,      // Flashcoin
	"ZEN":         121,      // Zencash
	"PUT":         122,      // Putincoin
	"ZNY":         123,      // BitZeny
	"UNIFY":       124,      // Unify
	"XST":         125,      // StealthCoin
	"BRK":         126,      // Breakout Coin
	"VC":          127,      // Vcash
	"XMR":         128,      // Monero
	"VOX":         129,      // Voxels
	"NAV":         130,      // NavCoin
	"FCT":         131,      // Factom Factoids
	"EC":          132,      // Factom Entry Credits
	"ZEC":         133,      // Zcash
	"LSK":         134,      // Lisk
	"STEEM":       135,      // Steem
	"XZC":         136,      // ZCoin
	"RBTC":        137,      // RSK
	"Giftblock":   138,      //
	"RPT":         139,      // RealPointCoin
	"LBC":         140,      // LBRY Credits
	"KMD":         141,      // Komodo
	"BSQ":         142,      // bisq Token
	"RIC":         143,      // Riecoin
	"XRP":         144,      // Ripple
	"BCH":         145,      // Bitcoin Cash
	"NEBL":        146,      // Neblio
	"ZCL":         147,      // ZClassic
	"XLM":         148,      // Stellar Lumens
	"NLC2":        149,      // NoLimitCoin2
	"WHL":         150,      // WhaleCoin
	"ERC":         151,      // EuropeCoin
	"DMD":         152,      // Diamond
	"BTM":         153,      // Bytom
	"BIO":         154,      // Biocoin
	"XWC":         155,      // Whitecoin
	"BTG":         156,      // Bitcoin Gold
	"BTC2X":       157,      // Bitcoin 2x
	"SSN":         158,      // SuperSkynet
	"TOA":         159,      // TOACoin
	"BTX":         160,      // Bitcore
	"ACC":         161,      // Adcoin
	"BCO":         162,      // Bridgecoin
	"ELLA":        163,      // Ellaism
	"PIRL":        164,      // Pirl
	"XRB":         165,      // RaiBlocks
	"VIVO":        166,      // Vivo
	"FRST":        167,      // Firstcoin
	"HNC":         168,      // Helleniccoin
	"BUZZ":        169,      // BUZZ
	"MBRS":        170,      // Ember
	"HSR":         171,      // Hcash
	"HTML":        172,      // HTMLCOIN
	"ODN":         173,      // Obsidian
	"ONX":         174,      // OnixCoin
	"RVN":         175,      // Ravencoin
	"GBX":         176,      // GoByte
	"BTCZ":        177,      // BitcoinZ
	"POA":         178,      // Poa
	"NYC":         179,      // NewYorkCoin
	"MXT":         180,      // MarteXcoin
	"WC":          181,      // Wincoin
	"MNX":         182,      // Minexcoin
	"BTCP":        183,      // Bitcoin Private
	"MUSIC":       184,      // Musicoin
	"BCA":         185,      // Bitcoin Atom
	"CRAVE":       186,      // Crave
	"STAK":        187,      // STRAKS
	"WBTC":        188,      // World Bitcoin
	"LCH":         189,      // LiteCash
	"EXCL":        190,      // ExclusiveCoin
	"Lynx":        191,      //
	"LCC":         192,      // LitecoinCash
	"XFE":         193,      // Feirm
	"EOS":         194,      // EOS
	"TRX":         195,      // Tron
	"KOBO":        196,      // Kobocoin
	"HUSH":        197,      // HUSH
	"BANANO":      198,      // Bananos
	"ETF":         199,      // ETF
	"OMNI":        200,      // Omni
	"BIFI":        201,      // BitcoinFile
	"UFO":         202,      // Uniform Fiscal Object
	"CNMC":        203,      // Cryptonodes
	"BCN":         204,      // Bytecoin
	"RIN":         205,      // Ringo
	"ATP":         206,      // PlatON
	"EVT":         207,      // everiToken
	"ATN":         208,      // ATN
	"BIS":         209,      // Bismuth
	"NEET":        210,      // NEETCOIN
	"BOPO":        211,      // BopoChain
	"OOT":         212,      // Utrum
	"XSPEC":       213,      // Spectrecoin
	"MONK":        214,      // Monkey Project
	"BOXY":        215,      // BoxyCoin
	"FLO":         216,      // Flo
	"MEC":         217,      // Megacoin
	"BTDX":        218,      // BitCloud
	"XAX":         219,      // Artax
	"ANON":        220,      // ANON
	"LTZ":         221,      // LitecoinZ
	"BITG":        222,      // Bitcoin Green
	"ASK":         223,      // AskCoin
	"SMART":       224,      // Smartcash
	"XUEZ":        225,      // XUEZ
	"HLM":         226,      // Helium
	"WEB":         227,      // Webchain
	"ACM":         228,      // Actinium
	"NOS":         229,      // NOS Stable Coins
	"BITC":        230,      // BitCash
	"HTH":         231,      // Help The Homeless Coin
	"TZC":         232,      // Trezarcoin
	"VAR":         233,      // Varda
	"IOV":         234,      // IOV
	"FIO":         235,      // FIO
	"BSV":         236,      // BitcoinSV
	"DXN":         237,      // DEXON
	"QRL":         238,      // Quantum Resistant Ledger
	"PCX":         239,      // ChainX
	"LOKI":        240,      // Loki
	"Imagewallet": 241,      //
	"NIM":         242,      // Nimiq
	"SOV":         243,      // Sovereign Coin
	"JCT":         244,      // Jibital Coin
	"SLP":         245,      // Simple Ledger Protocol
	"EWT":         246,      // Energy Web
	"UC":          247,      // Ulord
	"EXOS":        248,      // EXOS
	"ECA":         249,      // Electra
	"SOOM":        250,      // Soom
	"XRD":         251,      // Redstone
	"FREE":        252,      // FreeCoin
	"NPW":         253,      // NewPowerCoin
	"BST":         254,      // BlockStamp
	"SmartHoldem": 255,      //
	"NANO":        256,      // Bitcoin Nano
	"BTCC":        257,      // Bitcoin Core
	"Zen":         258,      // Protocol
	"ZEST":        259,      // Zest
	"ABT":         260,      // ArcBlock
	"PION":        261,      // Pion
	"DT3":         262,      // DreamTeam3
	"ZBUX":        263,      // Zbux
	"KPL":         264,      // Kepler
	"TPAY":        265,      // TokenPay
	"ZILLA":       266,      // ChainZilla
	"ANK":         267,      // Anker
	"BCC":         268,      // BCChain
	"HPB":         269,      // HPB
	"ONE":         270,      // ONE
	"SBC":         271,      // SBC
	"IPC":         272,      // IPChain
	"DMTC":        273,      // Dominantchain
	"OGC":         274,      // Onegram
	"SHIT":        275,      // Shitcoin
	"ANDES":       276,      // Andescoin
	"AREPA":       277,      // Arepacoin
	"BOLI":        278,      // Bolivarcoin
	"RIL":         279,      // Rilcoin
	"HTR":         280,      // Hathor Network
	"FCTID":       281,      // Factom ID
	"BRAVO":       282,      // BRAVO
	"ALGO":        283,      // Algorand
	"BZX":         284,      // Bitcoinzero
	"GXX":         285,      // GravityCoin
	"HEAT":        286,      // HEAT
	"XDN":         287,      // DigitalNote
	"FSN":         288,      // FUSION
	"CPC":         289,      // Capricoin
	"BOLD":        290,      // Bold
	"IOST":        291,      // IOST
	"TKEY":        292,      // Tkeycoin
	"USE":         293,      // Usechain
	"BCZ":         294,      // BitcoinCZ
	"IOC":         295,      // Iocoin
	"ASF":         296,      // Asofe
	"MASS":        297,      // MASS
	"FAIR":        298,      // FairCoin
	"NUKO":        299,      // Nekonium
	"GNX":         300,      // Genaro Network
	"DIVI":        301,      // Divi Project
	"CMT":         302,      // Community
	"EUNO":        303,      // EUNO
	"IOTX":        304,      // IoTeX
	"ONION":       305,      // DeepOnion
	"8BIT":        306,      // 8Bit
	"ATC":         307,      // AToken Coin
	"BTS":         308,      // Bitshares
	"CKB":         309,      // Nervos CKB
	"UGAS":        310,      // Ultrain
	"ADS":         311,      // Adshares
	"ARA":         312,      // Aura
	"ZIL":         313,      // Zilliqa
	"MOAC":        314,      // MOAC
	"SWTC":        315,      // SWTC
	"VNSC":        316,      // vnscoin
	"PLUG":        317,      // Pl^g
	"MAN":         318,      // Matrix AI Network
	"ECC":         319,      // ECCoin
	"RPD":         320,      // Rapids
	"RAP":         321,      // Rapture
	"GARD":        322,      // Hashgard
	"ZER":         323,      // Zero
	"EBST":        324,      // eBoost
	"SHARD":       325,      // Shard
	"LINDA":       326,      // Linda Coin
	"CMM":         327,      // Commercium
	"BLOCK":       328,      // Blocknet
	"AUDAX":       329,      // AUDAX
	"LUNA":        330,      // Terra
	"ZPM":         331,      // zPrime
	"KUVA":        332,      // Kuva Utility Note
	"MEM":         333,      // MemCoin
	"CS":          334,      // Credits
	"SWIFT":       335,      // SwiftCash
	"FIX":         336,      // FIX
	"CPC":         337,      // CPChain
	"VGO":         338,      // VirtualGoodsToken
	"DVT":         339,      // DeVault
	"N8V":         340,      // N8VCoin
	"MTNS":        341,      // OmotenashiCoin
	"BLAST":       342,      // BLAST
	"DCT":         343,      // DECENT
	"AUX":         344,      // Auxilium
	"USDP":        345,      // USDP
	"HTDF":        346,      // HTDF
	"YEC":         347,      // Ycash
	"QLC":         348,      // QLC Chain
	"TEA":         349,      // Icetea Blockchain
	"ARW":         350,      // ArrowChain
	"MDM":         351,      // Medium
	"CYB":         352,      // Cybex
	"LTO":         353,      // LTO Network
	"DOT":         354,      // Polkadot
	"AEON":        355,      // Aeon
	"RES":         356,      // Resistance
	"AYA":         357,      // Aryacoin
	"DAPS":        358,      // Dapscoin
	"CSC":         359,      // CasinoCoin
	"VSYS":        360,      // V Systems
	"NOLLAR":      361,      // Nollar
	"XNOS":        362,      // NOS
	"CPU":         363,      // CPUchain
	"LAMB":        364,      // Lambda Storage Chain
	"VCT":         365,      // ValueCyber
	"CZR":         366,      // Canonchain
	"ABBC":        367,      // ABBC
	"HET":         368,      // HET
	"XAS":         369,      // Asch
	"VDL":         370,      // Vidulum
	"MED":         371,      // MediBloc
	"ZVC":         372,      // ZVChain
	"VESTX":       373,      // Vestx
	"DBT":         374,      // DarkBit
	"SEOS":        375,      // SuperEOS
	"MXW":         376,      // Maxonrow
	"ZNZ":         377,      // ZENZO
	"XCX":         378,      // XChain
	"SOX":         379,      // SonicX
	"NYZO":        380,      // Nyzo
	"ULC":         381,      // ULCoin
	"RYO":         382,      // Ryo Currency
	"KAL":         383,      // Kaleidochain
	"XSN":         384,      // Stakenet
	"DOGEC":       385,      // DogeCash
	"BMV":         386,      // Bitcoin Matteo's Vision
	"QBC":         387,      // Quebecoin
	"IMG":         388,      // ImageCoin
	"QOS":         389,      // QOS
	"PKT":         390,      // PKT
	"LHD":         391,      // LitecoinHD
	"CENNZ":       392,      // CENNZnet
	"HSN":         393,      // Hyper Speed Network
	"CRO":         394,      // Crypto.com Chain
	"UMBRU":       395,      // Umbru
	"TON":         396,      // Telegram
	"NEAR":        397,      // NEAR Protocol
	"XPC":         398,      // XPChain
	"ZOC":         399,      // 01coin
	"NIX":         400,      // NIX
	"UC":          401,      // Utopiacoin
	"XBI":         404,      // XBI
	"AIN":         412,      // AIN
	"SLX":         416,      // SLX
	"NODE":        420,      // NodeHost
	"AION":        425,      // Aion
	"BC":          426,      // Bitcoin Confidential
	"PHR":         444,      // Phore
	"DIN":         447,      // Dinero
	"AE":          457,      // æternity
	"ETI":         464,      // EtherInc
	"VEO":         488,      // Amoveo
	"THETA":       500,      // Theta
	"SOL":         501,      // Solana
	"KOTO":        510,      // Koto
	"XRD":         512,      // Radiant
	"VEE":         516,      // Virtual Economy Era
	"LET":         518,      // Linkeye
	"BTCV":        520,      // BitcoinVIP
	"BU":          526,      // BUMO
	"YAP":         528,      // Yapstone
	"PRJ":         533,      // ProjectCoin
	"BCS":         555,      // Bitcoin Smart
	"LKR":         557,      // Lkrcoin
	"NTY":         561,      // Nexty
	"UTE":         600,      // Unit-e
	"SSP":         618,      // SmartShare
	"EAST":        625,      // Eastcoin
	"SFRX":        663,      // EtherGem Sapphire
	"ACT":         666,      // Achain
	"PRKL":        667,      // Perkle
	"SSC":         668,      // SelfSell
	"VEIL":        698,      // Veil
	"XDAI":        700,      // xDai
	"XTL":         713,      // Katal
	"BNB":         714,      // Binance
	"SIN":         715,      // Sinovate
	"BALLZ":       768,      // Ballzcoin
	"BTW":         777,      // Bitcoin World
	"BEET":        800,      // Beetle Coin
	"DST":         801,      // DSTRA
	"QVT":         808,      // Qvolta
	"VET":         818,      // VeChain Token
	"CLO":         820,      // Callisto
	"CRUZ":        831,      // cruzbit
	"DESM":        852,      // Desmos
	"ADF":         886,      // AD Token
	"NEO":         888,      // NEO
	"TOMO":        889,      // TOMO
	"XSEL":        890,      // Seln
	"LMO":         900,      // Lumeneo
	"META":        916,      // Metadium
	"TWINS":       970,      // TWINS
	"OKP":         996,      // OK Points
	"SUM":         997,      // Solidum
	"LBTC":        998,      // Lightning Bitcoin
	"BCD":         999,      // Bitcoin Diamond
	"BTN":         1000,     // Bitcoin New
	"TT":          1001,     // ThunderCore
	"BKT":         1002,     // BanKitt
	"ONE":         1023,     // HARMONY-ONE
	"ONT":         1024,     // Ontology
	"KEX":         1026,     // Kira Exchange Token
	"MCM":         1027,     // Mochimo
	"BBC":         1111,     // Big Bitcoin
	"RISE":        1120,     // RISE
	"CMT":         1122,     // CyberMiles Token
	"ETSC":        1128,     // Ethereum Social
	"CDY":         1145,     // Bitcoin Candy
	"DFC":         1337,     // Defcoin
	"HYC":         1397,     // Hycon
	"Taler":       1524,     //
	"BEAM":        1533,     // Beam
	"ELF":         1616,     // AELF
	"ATH":         1620,     // Atheios
	"BCX":         1688,     // BitcoinX
	"XTZ":         1729,     // Tezos
	"LBTC":        1776,     // Liquid BTC
	"ADA":         1815,     // Cardano
	"TES":         1856,     // Teslacoin
	"CLC":         1901,     // Classica
	"VIPS":        1919,     // VIPSTARCOIN
	"CITY":        1926,     // City Coin
	"XMX":         1977,     // Xuma
	"TRTL":        1984,     // TurtleCoin
	"EGEM":        1987,     // EtherGem
	"HODL":        1989,     // HOdlcoin
	"PHL":         1990,     // Placeholders
	"POLIS":       1997,     // Polis
	"XMCC":        1998,     // Monoeci
	"COLX":        1999,     // ColossusXT
	"GIN":         2000,     // GinCoin
	"MNP":         2001,     // MNPCoin
	"KIN":         2017,     // Kin
	"EOSC":        2018,     // EOSClassic
	"GBT":         2019,     // GoldBean Token
	"PKC":         2020,     // PKC
	"MCASH":       2048,     // MCashChain
	"TRUE":        2049,     // TrueChain
	"IoTE":        2112,     // IoTE
	"ASK":         2221,     // ASK
	"QTUM":        2301,     // QTUM
	"ETP":         2302,     // Metaverse
	"GXC":         2303,     // GXChain
	"CRP":         2304,     // CranePay
	"ELA":         2305,     // Elastos
	"SNOW":        2338,     // Snowblossom
	"AOA":         2570,     // Aurora
	"REOSC":       2894,     // REOSC Ecosystem
	"LUX":         3003,     // LUX
	"XHB":         3030,     // Hedera HBAR
	"COS":         3077,     // Contentos
	"DYN":         3381,     // Dynamic
	"SEQ":         3383,     // Sequence
	"DEO":         3552,     // Destocoin
	"DST":         3564,     // DeStream
	"NAS":         2718,     // Nebulas
	"BND":         2941,     // Blocknode
	"CCC":         3276,     // CodeChain
	"ROI":         3377,     // ROIcoin
	"IOTA":        4218,     // IOTA
	"AXE":         4242,     // Axe
	"FIC":         5248,     // FIC
	"HNS":         5353,     // Handshake
	"Stacks":      5757,     //
	"SLU":         5920,     // SILUBIUM
	"GO":          6060,     // GoChain GO
	"BPA":         6666,     // Bitcoin Pizza
	"SAFE":        6688,     // SAFE
	"ROGER":       6969,     // TheHolyrogerCoin
	"BTV":         7777,     // Bitvote
	"BTQ":         8339,     // BitcoinQuark
	"SBTC":        8888,     // Super Bitcoin
	"NULS":        8964,     // NULS
	"BTP":         8999,     // Bitcoin Pay
	"NRG":         9797,     // Energi
	"BTF":         9888,     // Bitcoin Faith
	"GOD":         9999,     // Bitcoin God
	"FO":          10000,    // FIBOS
	"BTR":         10291,    // Bitcoin Rhodium
	"ESS":         11111,    // Essentia One
	"IPOS":        12345,    // IPOS
	"BTY":         13107,    // BitYuan
	"YCC":         13108,    // Yuan Chain Coin
	"SDGO":        15845,    // SanDeGo
	"ARDR":        16754,    // Ardor
	"SAFE":        19165,    // Safecoin
	"ZEL":         19167,    // ZelCash
	"RITO":        19169,    // Ritocoin
	"XND":         20036,    // ndau
	"PWR":         22504,    // PWRcoin
	"BELL":        25252,    // Bellcoin
	"CHX":         25718,    // Own
	"ESN":         31102,    // EtherSocial Network
	"ThePower":    31337,    //
	"TEO":         33416,    // Trust Eth reOrigin
	"BTCS":        33878,    // Bitcoin Stake
	"BTT":         34952,    // ByteTrade
	"FXTC":        37992,    // FixedTradeCoin
	"AMA":         39321,    // Amabig
	"STASH":       49344,    // STASH
	"KETH":        65536,    // Krypton World
	"RYO":         88888,    // c0ban
	"WICC":        99999,    // Waykichain
	"AKA":         200625,   // Akroma
	"GENOM":       200665,   // GENOM
	"ATS":         246529,   // ARTIS sigma1
	"X42":         424242,   // x42
	"VITE":        666666,   // Vite
	"ILT":         1171337,  // iOlite
	"ETHO":        1313114,  // Ether-1
	"XERO":        1313500,  // Xerom
	"LAX":         1712144,  // LAPO
	"BCO":         5249353,  // BitcoinOre
	"BHD":         5249354,  // BitcoinHD
	"PTN":         5264462,  // PalletOne
	"WAN":         5718350,  // Wanchain
	"WAVES":       5741564,  // Waves
	"SEM":         7562605,  // Semux
	"ION":         7567736,  // ION
	"WGR":         7825266,  // WGR
	"OBSR":        7825267,  // OBServer
	"AQUA":        61717561, // Aquachain
	"kUSD":        91927009, // kUSD
	"FLUID":       99999998, // FluiChains
	"QKC":         99999999, // QuarkChain
}
