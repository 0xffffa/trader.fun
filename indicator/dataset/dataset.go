package dataset

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
	"trader.fun/pumpfun"
)

type Dataset struct {
	Captured      int
	pf            *pumpfun.Pumpfun
	dsLock        sync.Mutex
	coinsCaptured *cache.Cache
}

func (ds *Dataset) Capture(coin *pumpfun.Coin) error {
	_, found := ds.coinsCaptured.Get(coin.MintAddr.String())
	if found {
		return errors.New("already captured")
	}
	ds.coinsCaptured.Set(coin.MintAddr.String(), true, cache.DefaultExpiration)
	compiled := coin.Compile()
	coinPrice := coin.Price()
	time.Sleep(3 * time.Second)
	endPrice := coin.Price()
	answer := ds.checkChangePercentage(coinPrice, endPrice, 10)
	return ds.writeCapture(compiled, answer)
}

func (ds *Dataset) checkChangePercentage(oldValue, newValue, percentage float64) bool {
	// Calculate the absolute difference between the old and new values
	change := math.Abs(newValue - oldValue)

	// Calculate the threshold based on the percentage
	threshold := (percentage / 100) * oldValue

	// Check if the change is greater than the threshold
	return change > threshold
}

func (ds *Dataset) writeCapture(compiled []float64, answerBool bool) error {
	ds.dsLock.Lock()
	defer ds.dsLock.Unlock()

	var answer int
	if answerBool {
		answer = 1
	} else {
		answer = 0
	}

	compiledStr := ds.floatArrayToString(compiled)

	text := compiledStr + "=>" + fmt.Sprintf("%d", answer) + "\r\n"

	// Open the file in append mode, create it if it doesn't exist
	file, err := os.OpenFile("dataset.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write text to the file
	_, err = file.WriteString(text)
	if err != nil {
		return err

	}

	ds.Captured++

	return nil
}

func (ds *Dataset) floatArrayToString(arr []float64) string {
	var strArr []string
	for _, num := range arr {
		strArr = append(strArr, strconv.FormatFloat(num, 'f', -1, 64))
	}
	return strings.Join(strArr, ",")
}

func New(pf *pumpfun.Pumpfun) *Dataset {
	return &Dataset{
		pf:            pf,
		coinsCaptured: cache.New(3*time.Minute, 5*time.Minute),
	}
}
