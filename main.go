package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

type PV struct {
	K string `json:"k"`
	A string `json:"a"`
	sync.Mutex
}

type Node struct {
	CanSend bool
	Height  int
}

type BO map[int]Node
type BOS struct {
	LBO BO
	Max int64
	sync.Mutex
}

var bos BOS
var keys []PV
var keyFile = "keys.json"

type statsSend struct {
	start     time.Time
	allCount  int64
	failCount int64
	succCount int64
	sync.RWMutex
}

func (self *statsSend) Tps() string {
	return fmt.Sprintf("%d/s", self.succCount/(int64(time.Since(self.start).Seconds())))
}

func (self *statsSend) Spent() string {
	return time.Since(self.start).String()
}

var ss statsSend
var pageSize int

type CountStats struct {
	AllCount  int64
	SuccCount int64
	FailCount int64
	sync.Mutex
}

func load() {
	b, err := os.ReadFile(keyFile)
	if err != nil {
		log.Fatalln(keyFile, " read file error", err)
		return
	}
	_ = json.Unmarshal(b, &keys)
}

func createPrivkeys() {
	for i := 0; i < 3000; i++ {
		// 生成随机的私钥
		privateKey, err := crypto.GenerateKey()
		if err != nil {
			panic(err)
		}

		// 使用私钥生成地址
		address := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()

		// 将私钥转换为hex编码的字符串
		privateKeyStr := hexutil.Encode(crypto.FromECDSA(privateKey))

		// 打印生成的私钥、地址和助记词
		fmt.Printf("Private Key: %s\n", privateKeyStr)
		fmt.Printf("Address: %s\n", address)
		keys = append(keys, PV{
			K: privateKeyStr[2:],
			A: address,
		})
		b, _ := json.Marshal(keys)
		os.WriteFile(keyFile, b, 0755)
	}
}

func (this *PV) WaitTx(ctx context.Context, txid string, client *ethclient.Client) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, err := client.TransactionReceipt(ctx, common.HexToHash(txid))
			if err == nil { // block packed
				return
			}
		}
	}
}

func (this *PV) SendTx(ctx context.Context, index, i int, client *ethclient.Client,
	toAddress common.Address, value *big.Int, repeatTimes int64, wg *sync.WaitGroup, cs *CountStats) {
	if wg != nil {
		defer wg.Done()
	}
	this.Lock()
	defer this.Unlock()

	privateKey, err := crypto.HexToECDSA(this.K)
	if err != nil {
		log.Fatal(err)
	}
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		log.Fatal("cannot assert type: publicKey is not of type *ecdsa.PublicKey")
	}

	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	nonce, err := client.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		// log.Fatal(err)
		logrus.Error(err)
		return
	}

	gasLimit := uint64(21000) // in units
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		logrus.Error(err)
		return
	}
	var data []byte
	tx := types.NewTransaction(nonce, toAddress, value, gasLimit, gasPrice, data)

	chainID, err := client.ChainID(ctx)
	if err != nil {
		log.Fatal(err)
	}

	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), privateKey)
	if err != nil {
		log.Fatal(err)
	}
	err = client.SendTransaction(ctx, signedTx)
	if err != nil {
		if cs != nil {
			atomic.AddInt64(&cs.FailCount, 1)
		}

		logrus.WithFields(logrus.Fields{
			"miner":              index,
			"send index":         i,
			"account send times": repeatTimes,
			"Tx Hash":            signedTx.Hash().Hex(),
			"Error":              err.Error(),
		}).Debugf("Send Tx Error")
		return
	}
	if cs != nil {
		atomic.AddInt64(&cs.SuccCount, 1)
	}
	logrus.Debugf("miner:%d, index:%d, tx sent: %s", index, i, signedTx.Hash().Hex())
	if os.Getenv("waitTx") == "1" {
		this.WaitTx(ctx, signedTx.Hash().Hex(), client)
	}
}

var rpcClients = []*ethclient.Client{}
var RPCS []string
var JRPCS []string
var JAUTH []string

func InitRPC() {
	JAUTH = strings.Split(os.Getenv("JAUTH"), ",")
	JRPCS = strings.Split(os.Getenv("JRPC"), ",")
	bos = BOS{
		LBO: BO{},
	}
	for i, _ := range JRPCS {
		lbo := bos.LBO[i]
		lbo.Height = 0
		bos.LBO[i] = lbo
	}
	bos.Max = 0
	RPCS = strings.Split(os.Getenv("RPC"), ",")
	for _, v := range RPCS {
		if v != "" {
			c, err := ethclient.Dial(v)
			if err != nil {
				logrus.Error(err)
				continue
			}
			rpcClients = append(rpcClients, c)
		}

	}
}

var reorgBlocks int

var start int

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()
	logrus.SetFormatter(&logrus.JSONFormatter{})
	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(logrus.DebugLevel)
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatalf("Some error occured. Err: %s", err)
	}
	InitRPC()
	start, _ = strconv.Atoi(os.Getenv("start"))
	pageSize, _ = strconv.Atoi(os.Getenv("pageSize"))
	reorgBlocks, _ = strconv.Atoi(os.Getenv("reorgBlocks"))
	client := rpcClients[0]
	if len(os.Args) > 1 {
		action := os.Args[1]
		switch action {
		case "init":
			createPrivkeys()
			return
		}
	}
	load()

	ctx := context.Background()
	logrus.Infof("Send %d txs every miner / sencond, miner count:%d", pageSize, len(rpcClients))
	adminPV := PV{
		K: os.Getenv("PRIVATE_KEY"),
	}
	if os.Getenv("init") == "1" {
		for i, v := range keys {
			ba, _ := client.BalanceAt(context.Background(), common.HexToAddress(v.A), nil)
			if ba.Cmp(big.NewInt(2e17)) <= 0 {
				logrus.WithFields(logrus.Fields{
					"index": i,
				}).Info("init account balance")
				adminPV.SendTx(ctx, 0, i, client, common.HexToAddress(v.A), big.NewInt(2e18), 0, nil, nil)
			}
		}
	}
	ss.start = time.Now()
	wg := sync.WaitGroup{}
	for i := 0; i < len(rpcClients); i++ {
		wg.Add(1)
		go AccountSend(&wg, ctx, i)
	}
	wg.Add(1)
	go SendStats(&wg, ctx)

	wg.Add(1)
	go MempoolSize(&wg, ctx)
	wg.Wait()
}

// AccountSend(ctx context.Context, index int)
func AccountSend(wg *sync.WaitGroup, ctx context.Context, index int) {
	defer wg.Done()

	rpcClient := rpcClients[index]
	var to common.Address
	gap := len(keys) / len(RPCS)
	offset := index*gap + start
	logrus.Infof("accountsend-------------miner:%d account start:%d account end:%d",
		index, offset, offset+pageSize)
	repeat := int64(0)
	wg1 := sync.WaitGroup{}
	for {
		bos.Lock()
		if !bos.LBO[index].CanSend {
			bos.Unlock()
			logrus.Infof("-------------node:%d need handle current txpool", index)
			<-time.After(2 * time.Second)
			continue
		}
		bos.Unlock()
		repeat++
		cs := &CountStats{
			SuccCount: 0,
			FailCount: 0,
			AllCount:  0,
		}
		for i := offset; i < (offset + pageSize); i++ {
			select {
			case <-ctx.Done():
				return
			default:
				// v := keys[i]
				if i == len(keys)-1 {
					to = common.HexToAddress(keys[0].A)
				} else {
					to = common.HexToAddress(keys[i+1].A)
				}
				// fmt.Println(v.A, to)
				atomic.AddInt64(&cs.AllCount, 1)
				wg1.Add(1)
				go keys[i].SendTx(ctx, index, i, rpcClient, to, big.NewInt(1e1), repeat, &wg1, cs)
				<-time.After(time.Second / time.Duration(pageSize))
			}
		}
		wg1.Wait()
		logrus.WithFields(logrus.Fields{
			"miner":              index,
			"account send times": repeat,
			"allSend":            cs.AllCount,
			"Succ":               cs.SuccCount,
			"Fail":               cs.FailCount,
		}).Info("Account Send Stats")
		ss.RLock()
		atomic.AddInt64(&ss.allCount, cs.AllCount)
		atomic.AddInt64(&ss.succCount, cs.SuccCount)
		atomic.AddInt64(&ss.failCount, cs.FailCount)
		ss.RUnlock()
		// os.Exit(0)
	}
	//
}
func SendStats(wg *sync.WaitGroup, ctx context.Context) {
	defer wg.Done()
	t := time.NewTicker(time.Second * 10)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			ss.RLock()
			logrus.WithFields(logrus.Fields{
				"AllSend":  ss.allCount,
				"Succ":     ss.succCount,
				"Fail":     ss.failCount,
				"Spent":    ss.Spent(),
				"Send TPS": ss.Tps(),
			}).Info("Bench Send Stats")
			ss.RUnlock()
		}
	}
	//
}

type Txpool struct {
	Pending string `json:"pending"`
	Queued  string `json:"queued"`
}

type PendingCount struct {
	Result Txpool `json:"result"`
}
type BlockCount struct {
	Result int64 `json:"result"`
}

func LatestestBlocks(wg *sync.WaitGroup, ctx context.Context) {
	defer wg.Done()
	t := time.NewTicker(time.Second * 3)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			bos.Lock()
			for i, v := range JRPCS {
				var bc BlockCount
				b := RpcResult(v, JAUTH[i], "getBlockCount", []interface{}{}, fmt.Sprintf("%d", i))
				if b != nil {
					_ = json.Unmarshal(b, &bc)
					lbo := bos.LBO[i]
					lbo.Height = int(bc.Result)
					bos.LBO[i] = lbo
					if bc.Result > bos.Max {
						bos.Max = bc.Result
					}
				}
			}
			bos.Unlock()

		}
	}
	//
}

func MempoolSize(wg *sync.WaitGroup, ctx context.Context) {
	defer wg.Done()
	t := time.NewTicker(time.Second * 1)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			bos.Lock()
			for i, v := range RPCS {
				var bc PendingCount
				b := RpcResult(v, "", "txpool_status", []interface{}{}, fmt.Sprintf("%d", i))
				if b != nil {
					_ = json.Unmarshal(b, &bc)
					pi, _ := strconv.ParseUint(bc.Result.Pending, 16, 64)
					qi, _ := strconv.ParseUint(bc.Result.Queued, 16, 64)
					lbo := bos.LBO[i]
					lbo.CanSend = (pi + qi) < uint64(pageSize*len(RPCS))*3
					bos.LBO[i] = lbo
					logrus.Infof("-------------minertxpool:%d this node pending info %v , canSend :%v, max pending:%v", i, (pi + qi), bos.LBO[i].CanSend, uint64(pageSize*len(RPCS))*2)
				}
			}
			bos.Unlock()

		}
	}
	//
}
