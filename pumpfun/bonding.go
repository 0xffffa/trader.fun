package pumpfun

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// Pump Curve Constants
const (
	TokenDecimals           = 6
	LamportsPerSol          = 1_000_000_000
	VirtualTokenReservesPos = 0x08
	VirtualSolReservesPos   = 0x10
)

type BondingCurve struct {
	VirtualTokenReserves uint64
	VirtualSolReserves   uint64
	RealTokenReserves    uint64
	RealSolReserves      uint64
	TokenTotalSupply     uint64
	Complete             bool
}

func GetBondingCurveInfos(client *rpc.Client, bondingCurve solana.PublicKey) (*BondingCurve, error) {
	mint, err := client.GetAccountInfo(context.Background(), bondingCurve)

	if err != nil {
		return nil, err
	}

	num := uint64(6966180631402821399)
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, num)

	DataBytes := mint.Value.Data.GetBinary()

	if !bytes.Equal(DataBytes[:8], buf.Bytes()) {
		return nil, errors.New("unexpected discriminator")
	}

	Data, err := DecodeBondingData(DataBytes)

	if err != nil {
		return nil, err
	}
	return Data, nil
}

func DecodeBondingData(bondingCurveData []byte) (*BondingCurve, error) {
	data := bondingCurveData[8:]
	bc := BondingCurve{}

	buf := bytes.NewReader(data)

	err := binary.Read(buf, binary.LittleEndian, &bc.VirtualTokenReserves)
	if err != nil {
		return nil, fmt.Errorf("error reading VirtualTokenReserves: %v", err)
	}

	err = binary.Read(buf, binary.LittleEndian, &bc.VirtualSolReserves)
	if err != nil {
		return nil, fmt.Errorf("error reading VirtualSolReserves: %v", err)
	}

	err = binary.Read(buf, binary.LittleEndian, &bc.RealTokenReserves)
	if err != nil {
		return nil, fmt.Errorf("error reading RealTokenReserves: %v", err)
	}

	err = binary.Read(buf, binary.LittleEndian, &bc.RealSolReserves)
	if err != nil {
		return nil, fmt.Errorf("error reading RealSolReserves: %v", err)
	}

	err = binary.Read(buf, binary.LittleEndian, &bc.TokenTotalSupply)
	if err != nil {
		return nil, fmt.Errorf("error reading TokenTotalSupply: %v", err)
	}

	var complete byte
	err = binary.Read(buf, binary.LittleEndian, &complete)
	if err != nil {
		return nil, fmt.Errorf("error reading Complete: %v", err)
	}
	bc.Complete = complete != 0

	return &bc, nil
}

func PriceInSolFromBondingCurveAddress(client *rpc.Client, bondingCurveAddress string) (float64, error) {
	mint, err := client.GetAccountInfoWithOpts(context.Background(), solana.MPK(bondingCurveAddress), &rpc.GetAccountInfoOpts{Commitment: rpc.CommitmentConfirmed})

	if err != nil {
		return 0, err
	}

	DataBytes := mint.Value.Data.GetBinary()
	Data, err := DecodeBondingData(DataBytes)
	if err != nil {
		return 0, err
	}

	solTokenPrice := float64(float64(Data.VirtualSolReserves/solana.LAMPORTS_PER_SOL)) / float64(Data.VirtualTokenReserves) * 1000000
	solTokenPrice = math.Round(solTokenPrice*1e9) / 1e9

	return solTokenPrice, nil
}
