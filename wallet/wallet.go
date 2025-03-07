package wallet

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"

	"github.com/gagliardetto/solana-go"
	associatedtokenaccount "github.com/gagliardetto/solana-go/programs/associated-token-account"
	computeBudget "github.com/gagliardetto/solana-go/programs/compute-budget"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/rpc"
	"trader.fun/pumpfun"
)

const (
	historyFileName = "history.json"
)

type SolWallet struct {
	Wallet          *solana.Wallet
	RpcClient       *rpc.Client
	PurchaseHistory map[pumpfun.Coin]float64 `json:"purchaseHistory"`
	walletLock      sync.Mutex
}

func (sw *SolWallet) BuyToken(coin *pumpfun.Coin, solAmount, slippage float64) error {
	sw.walletLock.Lock()
	defer sw.walletLock.Unlock()

	walletAddress := sw.Wallet.PublicKey()

	TokenAddress, _, err := solana.FindAssociatedTokenAddress(walletAddress, coin.MintAddr)
	if err != nil {
		return err
	}

	BondingCurveData, err := pumpfun.GetBondingCurveInfos(sw.RpcClient, coin.TokenBondingCurve)
	if err != nil {
		return fmt.Errorf("error getting bonding curve data: %v", err)
	}

	if coin.AssociatedBondingCurve.IsZero() {
		coin.Metadata()
	}

	solTokenPrice := float64(float64(BondingCurveData.VirtualSolReserves/solana.LAMPORTS_PER_SOL)) / float64(BondingCurveData.VirtualTokenReserves) * 1000000
	solTokenPrice = math.Round(solTokenPrice*1e9) / 1e9

	TokenAmount := ((solAmount) / (solTokenPrice))
	TokenAmountInInt := uint64(TokenAmount * 1000000)
	solInWithSlippage := (solAmount) * (1 + slippage)
	lamportsInWithSlippage := uint64(solInWithSlippage * 1000000000)
	createATAInstruction := associatedtokenaccount.NewCreateInstruction(
		sw.Wallet.PublicKey(),
		sw.Wallet.PublicKey(),
		coin.MintAddr,
	).Build()

	accountKeys := solana.AccountMetaSlice{
		{PublicKey: solana.MustPublicKeyFromBase58("4wTV1YmiEkRvAtNtsSGPtUrqRYQMe5SKy2uB4Jjaxnjf"), IsSigner: false, IsWritable: false},
		{PublicKey: solana.MustPublicKeyFromBase58("CebN5WGQ4jvEPvsVU4EoHEpgzq1VV7AbicfhtW4xC9iM"), IsSigner: false, IsWritable: true},
		{PublicKey: coin.MintAddr, IsSigner: false, IsWritable: false},
		{PublicKey: coin.TokenBondingCurve, IsSigner: false, IsWritable: true},
		{PublicKey: coin.AssociatedBondingCurve, IsSigner: false, IsWritable: true},
		{PublicKey: TokenAddress, IsSigner: false, IsWritable: true},
		{PublicKey: sw.Wallet.PublicKey(), IsSigner: true, IsWritable: true},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
		{PublicKey: solana.TokenProgramID, IsSigner: false, IsWritable: false},
		{PublicKey: solana.SysVarRentPubkey, IsSigner: false, IsWritable: false},
		{PublicKey: solana.MustPublicKeyFromBase58("Ce6TQqeHC9p8KetsN6JsjHK7UTZk7nasjjnr7XxXp9F1"), IsSigner: false, IsWritable: false},
		{PublicKey: solana.MustPublicKeyFromBase58("6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P"), IsSigner: false, IsWritable: false},
	}

	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint64(16927863322537952870)) // buy discriminatior
	binary.Write(&buf, binary.LittleEndian, uint64(TokenAmountInInt))
	binary.Write(&buf, binary.LittleEndian, lamportsInWithSlippage)
	data := buf.Bytes()

	BuyInstruction := solana.NewInstruction(
		solana.MustPublicKeyFromBase58("6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P"),
		accountKeys,
		data,
	)

	computeBudgetInstruction := computeBudget.NewSetComputeUnitPriceInstruction(uint64(250000)).Build()
	computeBudgetInstruction2 := computeBudget.NewSetComputeUnitLimitInstruction(uint32(100000)).Build()

	// Create a transaction
	instructions := []solana.Instruction{
		computeBudgetInstruction,
		computeBudgetInstruction2,
		createATAInstruction,
		BuyInstruction,
	}

	blockHash, err := sw.RpcClient.GetRecentBlockhash(context.Background(), rpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("error getting recent blockhash: %v", err)
	}

	tx, err := solana.NewTransaction(instructions, blockHash.Value.Blockhash, solana.TransactionPayer(sw.Wallet.PublicKey()))
	if err != nil {
		return fmt.Errorf("error creating transaction: %v", err)
	}

	privateKeyGetter := func(pubKey solana.PublicKey) *solana.PrivateKey {
		if pubKey == sw.Wallet.PublicKey() {
			return &sw.Wallet.PrivateKey
		}
		return nil
	}

	tx.Sign(privateKeyGetter)

	opts := rpc.TransactionOpts{
		SkipPreflight:       true,
		PreflightCommitment: rpc.CommitmentFinalized,
	}

	// txID from this function
	if _, err := sw.RpcClient.SendTransactionWithOpts(context.Background(), tx, opts); err != nil {
		return fmt.Errorf("error sending transaction: %v", err)
	} else {
		if amount, ok := sw.PurchaseHistory[*coin]; ok {
			sw.PurchaseHistory[*coin] = amount + TokenAmount
		} else {
			sw.PurchaseHistory[*coin] = TokenAmount
		}
		return sw.SaveHistory()
	}
}

func (sw *SolWallet) SellToken(coin *pumpfun.Coin, percentage, slippage float64) error {
	sw.walletLock.Lock()
	defer sw.walletLock.Unlock()

	walletAddress := sw.Wallet.PublicKey()

	if percentage > 100 || percentage < 0 {
		return errors.New("sell percentage must be between 0-100")
	}

	totalHoldings, holdingToken := sw.PurchaseHistory[*coin]
	if !holdingToken {
		return errors.New("you must be holding this token to sell it")
	}

	BondingCurveData, err := pumpfun.GetBondingCurveInfos(sw.RpcClient, coin.TokenBondingCurve)
	if err != nil {
		return err
	}

	TokenAddress, _, err := solana.FindAssociatedTokenAddress(walletAddress, coin.MintAddr)
	if err != nil {
		return err
	}

	if coin.AssociatedBondingCurve.IsZero() {
		coin.Metadata()
	}

	sellAmount := totalHoldings * (percentage / 100)

	solTokenPrice := float64(float64(BondingCurveData.VirtualSolReserves/solana.LAMPORTS_PER_SOL)) / float64(BondingCurveData.VirtualTokenReserves) * 1000000
	solTokenPrice = math.Round(solTokenPrice*1e9) / 1e9

	TokenAmountInInt := uint64(sellAmount) * 1000000
	solOut := (sellAmount * solTokenPrice)
	solOutWithSlippage := solOut * (1 - slippage)
	lamportsOutWithSlippage := uint64(solOutWithSlippage * 1000000000)

	accountKeys := solana.AccountMetaSlice{
		{PublicKey: solana.MustPublicKeyFromBase58("4wTV1YmiEkRvAtNtsSGPtUrqRYQMe5SKy2uB4Jjaxnjf"), IsSigner: false, IsWritable: false},
		{PublicKey: solana.MustPublicKeyFromBase58("CebN5WGQ4jvEPvsVU4EoHEpgzq1VV7AbicfhtW4xC9iM"), IsSigner: false, IsWritable: true},
		{PublicKey: coin.MintAddr, IsSigner: false, IsWritable: false},
		{PublicKey: coin.TokenBondingCurve, IsSigner: false, IsWritable: true},
		{PublicKey: coin.AssociatedBondingCurve, IsSigner: false, IsWritable: true},
		{PublicKey: TokenAddress, IsSigner: false, IsWritable: true},
		{PublicKey: sw.Wallet.PublicKey(), IsSigner: true, IsWritable: true},
		{PublicKey: solana.SystemProgramID, IsSigner: false, IsWritable: false},
		{PublicKey: solana.MustPublicKeyFromBase58("ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL"), IsSigner: false, IsWritable: false},
		{PublicKey: solana.TokenProgramID, IsSigner: false, IsWritable: false},
		{PublicKey: solana.MustPublicKeyFromBase58("Ce6TQqeHC9p8KetsN6JsjHK7UTZk7nasjjnr7XxXp9F1"), IsSigner: false, IsWritable: false},
		{PublicKey: solana.MustPublicKeyFromBase58("6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P"), IsSigner: false, IsWritable: false},
	}

	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint64(12502976635542562355))
	binary.Write(&buf, binary.LittleEndian, uint64(TokenAmountInInt))
	binary.Write(&buf, binary.LittleEndian, lamportsOutWithSlippage)
	data := buf.Bytes()

	SellInstruction := solana.NewInstruction(
		solana.MustPublicKeyFromBase58("6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P"),
		accountKeys,
		data,
	)

	instructions := []solana.Instruction{
		SellInstruction,
	}

	blockHash, err := sw.RpcClient.GetRecentBlockhash(context.Background(), rpc.CommitmentFinalized)
	if err != nil {
		return err
	}

	if _, err := solana.NewTransaction(instructions, blockHash.Value.Blockhash, solana.TransactionPayer(sw.Wallet.PublicKey())); err != nil {
		return err
	}

	if totalHoldings-sellAmount <= 0 {
		delete(sw.PurchaseHistory, *coin)
	} else {
		sw.PurchaseHistory[*coin] = totalHoldings - sellAmount
	}

	return sw.SaveHistory()
}

func (sw *SolWallet) SellAll(slippage float64) error {
	for coin, _ := range sw.PurchaseHistory {
		if err := sw.SellToken(&coin, 100, slippage); err != nil { // sell 100% of every token
			return err
		}
	}
	return nil
}

func (sw *SolWallet) Withdrawl(address string, solAmount float64) error {

	if solBalance, err := sw.SolBalance(); err != nil {
		return err
	} else if solAmount >= solBalance {
		return errors.New("not enough sol in wallet to complete this transaction")
	}

	lamports := uint64(solAmount * float64(solana.LAMPORTS_PER_SOL))

	recent, err := sw.RpcClient.GetLatestBlockhash(context.TODO(), rpc.CommitmentFinalized)
	if err != nil {
		return err
	}

	tx, err := solana.NewTransaction(
		[]solana.Instruction{
			system.NewTransferInstruction(
				lamports,
				sw.Wallet.PublicKey(),
				solana.MPK(address),
			).Build(),
		},
		recent.Value.Blockhash,
		solana.TransactionPayer(sw.Wallet.PublicKey()),
	)
	if err != nil {
		return err
	}

	// Sign transaction
	if _, err := tx.Sign(
		func(key solana.PublicKey) *solana.PrivateKey {
			if sw.Wallet.PublicKey().Equals(key) {
				return &sw.Wallet.PrivateKey
			}
			return nil
		},
	); err != nil {
		return err
	}

	// send transaction
	if _, err := sw.RpcClient.SendTransaction(
		context.Background(),
		tx,
	); err != nil {
		return err
	}

	return nil
}

func (sw *SolWallet) SolBalance() (float64, error) {
	balanceResult, err := sw.RpcClient.GetBalance(context.Background(), sw.Wallet.PublicKey(), rpc.CommitmentConfirmed)
	if err != nil {
		return 0, err
	}

	return float64(balanceResult.Value) / float64(solana.LAMPORTS_PER_SOL), nil
}

func (sw *SolWallet) SaveHistory() error {
	data, err := json.MarshalIndent(sw.PurchaseHistory, "", "  ") // MarshalIndent for pretty-printed JSON
	if err != nil {
		return fmt.Errorf("error marshalling map to JSON: %v", err)
	}

	file, err := os.OpenFile(historyFileName, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("error creating file: %v", err)
	}
	defer file.Close()

	// Write the JSON data to the file
	_, err = file.Write(data)
	if err != nil {
		return fmt.Errorf("error writing to file: %v", err)
	}

	return nil
}

func (sw *SolWallet) LoadHistory() error {

	// Open the file
	file, err := os.Open(historyFileName)
	if err != nil {
		return fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close()

	// Decode the JSON data into a map
	var m map[pumpfun.Coin]float64
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&m)
	if err != nil {
		return fmt.Errorf("error decoding JSON: %v", err)
	}

	sw.PurchaseHistory = m

	return nil
}

func New(RpcClient *rpc.Client, privateKey string) *SolWallet {
	var wallet *solana.Wallet
	if len(privateKey) > 0 {
		wallet, _ = solana.WalletFromPrivateKeyBase58(privateKey)
	} else {
		wallet = solana.NewWallet()
	}

	sw := &SolWallet{
		Wallet: wallet,
	}
	sw.LoadHistory()

	return sw
}
