package mnemonic

import (
	"bytes"
	"testing"
	"time"
)

func TestFindWordIndex(t *testing.T) {
	for i := range wordList {
		j, err := wordIndex(wordList[i])
		if err != nil {
			t.Fatal(err)
		}
		if i != int(j) {
			t.Fatalf("wrong index %d returned for %q. expected %d", j, wordList[i], i)
		}
	}

	if _, err := wordIndex("blah"); err == nil {
		t.Fatal("no error for blah")
	}

	if _, err := wordIndex("aaa"); err == nil {
		t.Fatal("no error for aaa")
	}

	if _, err := wordIndex("zzz"); err == nil {
		t.Fatal("no error for zzz")
	}
}

func TestEncodeDecode(t *testing.T) {
	for i := 0; i < 1000; i++ {
		ogEntropy, mnemonic := New()
		reEntropy, stamp, err := DecodeMnemonic(mnemonic)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(reEntropy, ogEntropy) {
			t.Fatal("failed to recover entropy")
		}
		if stamp.Unix()/secondsPerDay != time.Now().Unix()/secondsPerDay {
			t.Fatalf("time not recovered")
		}
	}
}
