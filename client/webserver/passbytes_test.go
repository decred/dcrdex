package webserver

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

func TestPasswordUmarshallingAsBytes(t *testing.T) {
	checkDirectUnmarshal := func(password string) error {
		// marshal to json first
		pwJSON, err := json.Marshal(password)
		if err != nil {
			return err
		}
		// unmarshal back from json
		var pwFromJSON PassBytes
		if err = json.Unmarshal(pwJSON, &pwFromJSON); err != nil {
			return err
		}
		// confirm accuracy of unmarshal
		if password != pwFromJSON.String() {
			return fmt.Errorf("original pw: %s \tmarshalled to %s \t unmarshalled to %s", password,
				string(pwJSON), pwFromJSON)
		}
		return nil
	}

	checkUnmarshalAsBodyProperty := func(password string) error {
		// marshal to json first
		originalData := struct {
			Password string `json:"pass"`
		}{
			Password: password,
		}
		jsonBody, err := json.Marshal(originalData)
		if err != nil {
			return err
		}
		// unmarshal back from json
		var restoredData = new(struct {
			Password PassBytes `json:"pass"`
		})
		if err = json.Unmarshal(jsonBody, restoredData); err != nil {
			return err
		}
		// confirm accuracy of unmarshal
		if password != restoredData.Password.String() {
			return fmt.Errorf("original body: %v \tmarshalled to %s \t unmarshalled to %v", originalData,
				string(jsonBody), restoredData)
		}
		return nil
	}

	var err error
	passwordUnmarshalledCorrectly := func(password string) bool {
		if err = checkDirectUnmarshal(password); err != nil {
			return false
		}
		err = checkUnmarshalAsBodyProperty(password)
		return err == nil
	}

	for i := 0; i < 100; i++ {
		if ok := passwordUnmarshalledCorrectly(randomPassword()); !ok {
			t.Fatal(err)
		}
	}
}

func randomPassword() string {
	rand.Seed(time.Now().UnixNano())
	digits := "0123456789"
	specials := "~=+%^*/()[]{}/!@#$?|\"\\&-_<>',.;"
	all := "ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
		"abcdefghijklmnopqrstuvwxyz" +
		digits + specials
	length := 24
	buf := make([]byte, length)
	buf[0] = digits[rand.Intn(len(digits))]
	buf[1] = specials[rand.Intn(len(specials))]
	for i := 2; i < length; i++ {
		buf[i] = all[rand.Intn(len(all))]
	}
	rand.Shuffle(len(buf), func(i, j int) {
		buf[i], buf[j] = buf[j], buf[i]
	})
	return string(buf)
}
