// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"context"
	"encoding/csv"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/btc"
	"decred.org/dcrdex/server/asset/dcr"
	"decred.org/dcrdex/server/asset/ltc"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg"
)

// We do not need a Backend Setup, just the Drivers to call DecodeCoinID. While
// we can do this with asset.DecodeCoinID(assetID, coinID), doing the following
// drastically reduces the locking/unlocking (asset.driversMtx) that would be
// required to decode coin IDs, and we will likely be doing many.
var assets = map[uint32]asset.Driver{
	0:  &btc.Driver{},
	2:  &ltc.Driver{},
	42: &dcr.Driver{},
}

var dbhost = flag.String("host", "/run/postgresql", "pg host") // default to unix socket, but 127.0.0.1 would be common too
var dbuser = flag.String("user", "dcrdex", "db username")
var dbpass = flag.String("pass", "", "db password")
var dbname = flag.String("dbname", "dcrdex", "db name")
var dbport = flag.Int("port", 5432, "db port")
var base = flag.Uint("base", 42, "market base asset id")
var quote = flag.Uint("quote", 0, "market quote asset id")
var acct = flag.String("acct", "", "filter for dex account") // default is all accounts

// MatchData supplements a db.MatchData with decoded swap and redeem coins.
type MatchData struct {
	db.MatchData
	MakerSwap   string
	TakerSwap   string
	MakerRedeem string
	TakerRedeem string
}

func convertMatchData(baseAsset, quoteAsset asset.Driver, md *db.MatchDataWithCoins) *MatchData {
	matchData := MatchData{
		MatchData: md.MatchData,
	}
	// asset0 is the maker swap / taker redeem asset.
	// asset1 is the taker swap / maker redeem asset.
	// Maker selling means asset 0 is base; asset 1 is quote.
	asset0, asset1 := baseAsset, quoteAsset
	if md.TakerSell {
		asset0, asset1 = quoteAsset, baseAsset
	}
	if len(md.MakerSwapCoin) > 0 {
		coinStr, err := asset0.DecodeCoinID(md.MakerSwapCoin)
		if err != nil {
			fmt.Printf("Unable to decode coin %x: %v\n", md.MakerSwapCoin, err)
			// leave empty and keep chugging
		}
		matchData.MakerSwap = coinStr
	}
	if len(md.TakerSwapCoin) > 0 {
		coinStr, err := asset1.DecodeCoinID(md.TakerSwapCoin)
		if err != nil {
			fmt.Printf("Unable to decode coin %x: %v\n", md.TakerSwapCoin, err)
		}
		matchData.TakerSwap = coinStr
	}
	if len(md.MakerRedeemCoin) > 0 {
		coinStr, err := asset0.DecodeCoinID(md.MakerRedeemCoin)
		if err != nil {
			fmt.Printf("Unable to decode coin %x: %v\n", md.MakerRedeemCoin, err)
		}
		matchData.MakerRedeem = coinStr
	}
	if len(md.TakerRedeemCoin) > 0 {
		coinStr, err := asset1.DecodeCoinID(md.TakerRedeemCoin)
		if err != nil {
			fmt.Printf("Unable to decode coin %x: %v\n", md.TakerRedeemCoin, err)
		}
		matchData.TakerRedeem = coinStr
	}

	return &matchData
}

// MarketMatchesStreaming streams all matches for market with base and quote
// through a MatchData processing function, which is a wrapper around the
// provided function and convertMatchData. The provided function should do two
// main things: (1) apply some filtering, and (2) write the match data out
// somewhere, which in this app is a CSV file.
func MarketMatchesStreaming(storage db.DEXArchivist, base, quote uint32, includeInactive bool, N int64, f func(*MatchData) error) (int, error) {
	baseAsset := assets[base]
	if baseAsset == nil {
		return 0, fmt.Errorf("asset %d not found", base)
	}
	quoteAsset := assets[quote]
	if quoteAsset == nil {
		return 0, fmt.Errorf("asset %d not found", quote)
	}
	fDB := func(md *db.MatchDataWithCoins) error {
		matchData := convertMatchData(baseAsset, quoteAsset, md)
		return f(matchData)
	}
	return storage.MarketMatchesStreaming(base, quote, includeInactive, N, fDB)
}

func main() {
	if err := mainCore(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	os.Exit(0)
}

func mainCore() error {
	ctx, quit := context.WithCancel(context.Background())
	defer quit()
	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt)
	go func() {
		<-killChan
		quit()
		fmt.Println("Shutting down...")
	}()

	flag.Parse()

	base, quote := uint32(*base), uint32(*quote)
	name, err := dex.MarketName(base, quote)
	if err != nil {
		return err
	}
	mkt := &dex.MarketInfo{
		Name:  name,
		Base:  base,
		Quote: quote,
	}

	pgCfg := &pg.Config{
		Host:      *dbhost,
		Port:      strconv.Itoa(*dbport),
		User:      *dbuser,
		Pass:      *dbpass,
		DBName:    *dbname,
		MarketCfg: []*dex.MarketInfo{mkt},
	}
	archiver, err := pg.NewArchiverForRead(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer archiver.Close()

	var acctID account.AccountID
	var haveAccount bool
	switch len(*acct) {
	case 0: // no account filter
	case account.HashSize * 2:
		acctB, err := hex.DecodeString(*acct)
		if err != nil {
			return err
		}
		copy(acctID[:], acctB)
		haveAccount = true
	default:
		return fmt.Errorf("bad acct ID %v", *acct)
	}

	csvfile, err := os.Create(fmt.Sprintf("acct_matches_%v.csv", acctID))
	if err != nil {
		return fmt.Errorf("error creating csv file: %w", err)
	}
	defer csvfile.Close()

	csvwriter := csv.NewWriter(csvfile)
	defer csvwriter.Flush()

	err = csvwriter.Write([]string{"unixtime", "maker", "taker", "quantity", "rate",
		"isTakerSell", "makerSwapTx", "makerSwapVout", "makerRedeemTx", "makerRedeemVin",
		"takerSwapTx", "takerSwapVout", "takerRedeemTx", "takerRedeemVin"})
	if err != nil {
		return fmt.Errorf("ERROR: csvwriter.Write failed: %w", err)
	}

	splitTx := func(txinout string) (tx, vinout string, err error) {
		if txinout == "" {
			return // ok
		}
		txsplit := strings.Split(txinout, ":")
		if len(txsplit) != 2 {
			err = fmt.Errorf("txinout (%s) not formatted as a txin/out", txinout)
			return
		}
		_, err = strconv.ParseUint(txsplit[1], 10, 32)
		if err != nil {
			err = fmt.Errorf("strconv.ParseUint(%s): %w", txsplit[1], err)
			return
		}
		return txsplit[0], txsplit[1], nil
	}

	_, err = MarketMatchesStreaming(archiver, base, quote, true, -1, func(md *MatchData) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if haveAccount && (md.MakerAcct != acctID && md.TakerAcct != acctID) {
			return nil
		}

		makerSwapTx, makerSwapVout, err := splitTx(md.MakerSwap)
		if err != nil {
			return fmt.Errorf("strings.Split(%s): %w", md.MakerSwap, err)
		}
		makerRedeemTx, makerRedeemVin, err := splitTx(md.MakerRedeem)
		if err != nil {
			return fmt.Errorf("strings.Split(%s): %w", md.MakerRedeem, err)
		}
		takerSwapTx, takerSwapVout, err := splitTx(md.TakerSwap)
		if err != nil {
			return fmt.Errorf("strings.Split(%s): %w", md.TakerSwap, err)
		}
		takerRedeemTx, takerRedeemVin, err := splitTx(md.TakerRedeem)
		if err != nil {
			return fmt.Errorf("strings.Split(%s): %w", md.TakerRedeem, err)
		}

		err = csvwriter.Write([]string{
			strconv.FormatUint(md.Epoch.Idx*md.Epoch.Dur/1000, 10),
			md.MakerAcct.String(),
			md.TakerAcct.String(),
			strconv.FormatInt(int64(md.Quantity)/1e8, 10),
			strconv.FormatFloat(float64(md.Rate)/1e8, 'f', -1, 64),
			strconv.FormatBool(md.TakerSell),
			makerSwapTx, makerSwapVout, makerRedeemTx, makerRedeemVin,
			takerSwapTx, takerSwapVout, takerRedeemTx, takerRedeemVin,
		})
		if err != nil {
			return fmt.Errorf("csvwriter.Write: %w", err)
		}
		return nil
	})

	return err
}
