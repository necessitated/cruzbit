package cruzbit

import (
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type KeyState struct {
	label    string
	memo     string
	revision uint
	time     int64
}

type Indexer struct {
	blockStore    BlockStorage
	ledger        Ledger
	processor     *Processor
	latestBlockID BlockID
	latestHeight  int64
	keyState      map[string]*KeyState
	directories   map[string]string
	dirBalances   map[string]map[string]int64
	dirGraphs     map[string]*Graph
	shutdownChan  chan struct{}
	wg            sync.WaitGroup
}

func NewIndexer(
	blockStore BlockStorage,
	ledger Ledger,
	processor *Processor,
	genesisBlockID BlockID,
) *Indexer {
	return &Indexer{
		blockStore:    blockStore,
		ledger:        ledger,
		processor:     processor,
		latestBlockID: genesisBlockID,
		latestHeight:  0,
		keyState:      make(map[string]*KeyState),
		directories:   make(map[string]string),
		dirBalances:   make(map[string]map[string]int64),
		dirGraphs:     make(map[string]*Graph),
		shutdownChan:  make(chan struct{}),
	}
}

// Run executes the indexer's main loop in its own goroutine.
func (idx *Indexer) Run() {
	idx.wg.Add(1)
	go idx.run()
}

func (idx *Indexer) run() {
	defer idx.wg.Done()

	ticker := time.NewTicker(30 * time.Second)

	// don't start indexing until we think we're synced.
	// we're just wasting time and slowing down the sync otherwise
	ibd, _, err := IsInitialBlockDownload(idx.ledger, idx.blockStore)
	if err != nil {
		panic(err)
	}
	if ibd {
		log.Printf("Indexer waiting for blockchain sync\n")
	ready:
		for {
			select {
			case _, ok := <-idx.shutdownChan:
				if !ok {
					log.Printf("Indexer shutting down...\n")
					return
				}
			case <-ticker.C:
				var err error
				ibd, _, err = IsInitialBlockDownload(idx.ledger, idx.blockStore)
				if err != nil {
					panic(err)
				}
				if !ibd {
					// time to start indexing
					break ready
				}
			}
		}
	}

	ticker.Stop()

	header, _, err := idx.blockStore.GetBlockHeader(idx.latestBlockID)
	if err != nil {
		log.Println(err)
		return
	}
	if header == nil {
		// don't have it
		log.Println(err)
		return
	}
	branchType, err := idx.ledger.GetBranchType(idx.latestBlockID)
	if err != nil {
		log.Println(err)
		return
	}
	if branchType != MAIN {
		// not on the main branch
		log.Println(err)
		return
	}

	var height int64 = header.Height
	for {
		nextID, err := idx.ledger.GetBlockIDForHeight(height)
		if err != nil {
			log.Println(err)
			return
		}
		if nextID == nil {
			height -= 1
			break
		}

		block, err := idx.blockStore.GetBlock(*nextID)
		if err != nil {
			// not found
			log.Println(err)
			return
		}

		if block == nil {
			// not found
			log.Printf("No block found with ID %v", nextID)
			return
		}

		idx.indexTransactions(block, *nextID, true)

		height += 1
	}

	log.Printf("Finished indexing at height %v", idx.latestHeight)
	log.Printf("Latest indexed blockID: %v", idx.latestBlockID)

	idx.rankGraph()

	// register for tip changes
	tipChangeChan := make(chan TipChange, 1)
	idx.processor.RegisterForTipChange(tipChangeChan)
	defer idx.processor.UnregisterForTipChange(tipChangeChan)

	for {
		select {
		case tip := <-tipChangeChan:
			log.Printf("Indexer received notice of new tip block: %s at height: %d\n", tip.BlockID, tip.Block.Header.Height)
			idx.indexTransactions(tip.Block, tip.BlockID, tip.Connect) //Todo: Make sure no transaction is skipped.
			if !tip.More {
				idx.rankGraph()
			}
		case _, ok := <-idx.shutdownChan:
			if !ok {
				log.Printf("Indexer shutting down...\n")
				return
			}
		}
	}
}

func inflateNodes(pubKey string) (bool, string, []string, uint) {
	//omit the revision from the pubKey/instruction for validation
	trimmed := strings.TrimRight(pubKey, "/+0=")
	splitPK := strings.Split(trimmed, "/")

	if len(splitPK) == 0 || splitPK[0] == "" {
		return false, "", nil, 0
	}

	for i := 0; i < len(splitPK); i++ {
		if splitPK[i] == "" {
			return false, "", append([]string{}, pubKey), 0
		}
	}

	//reset to include the revision
	trimmed = strings.TrimRight(pubKey, "0=")
	splitPK = strings.Split(trimmed, "/")

	rootdir := splitPK[0]
	nodes := splitPK
	revision := 0

	if last := nodes[len(nodes)-1]; strings.Trim(last, "+") == "" {
		revision = len(last)
		nodes = nodes[:len(nodes)-1] // remove the revision from the nodes
	}

	//append implicit revision (node/+++content/+++) to node identifier (node/+++)
	for i := 0; i < len(nodes); i++ {
		node := nodes[i]

		if j := i + 1; j < len(nodes) {
			next := nodes[j]
			if strings.HasPrefix(next, "+") {
				//get prefix
				prefix := strings.Split(next, strings.Trim(next, "+"))[0]
				node = node + "/" + prefix
			}
		}

		nodes[i] = node
	}

	return true, rootdir, nodes, uint(revision)
}

func isLabelling(key string) (bool, string) {
	if strings.HasPrefix(key, "//") {
		re := regexp.MustCompile(`//([^/]+)//`)
		trimmed := strings.TrimRight(key, "0=")
		matches := re.FindStringSubmatch(trimmed)
		if len(matches) > 1 {
			return true, strings.ReplaceAll(strings.Trim(trimmed, "/"), "+", " ")
		}
	}

	return false, ""
}

func (idx *Indexer) indexTransactions(block *Block, id BlockID, increment bool) {

	idx.latestBlockID = id
	idx.latestHeight = block.Header.Height

	for t := 0; t < len(block.Transactions); t++ {
		txn := block.Transactions[t]

		txid, err := txn.ID()
		if err != nil {
			log.Printf("Error computing transaction ID: %v", err)
			continue
		}

		txnFrom := pubKeyToString(txn.From)
		txnTo := pubKeyToString(txn.To)

		/*
			TODO: reversal, when Block disconnected
			When Block disconnected; reverse all applicable transactions from the graph>>>>>>>>>>>>>>>>>>>
		*/
		incrementBy := int64(0)

		if increment {
			incrementBy = txn.Amount
		}

		if isLabl, label := isLabelling(txnTo); isLabl {

			if txn.From == nil {
				directoryID := txid.String()
				idx.directories[directoryID] = label
				idx.dirGraphs[directoryID] = NewGraph()
				idx.dirBalances[directoryID] = make(map[string]int64)
			} else {
				//Capture label: "SenderKey" -> "//DirectoryLabel//0000000000000000000000000000="

				if _, ok := idx.keyState[txnFrom]; !ok {
					idx.keyState[txnFrom] = &KeyState{}
				}
				idx.keyState[txnFrom].label = label
				memo := strings.TrimSpace(txn.Memo)
				idx.keyState[txnFrom].memo = memo
			}

			continue
		}

		trimmedSysMemo := strings.Trim(txn.Memo, "/")

		if _, ok := idx.directories[trimmedSysMemo]; ok {

			if balances, ok := idx.dirBalances[trimmedSysMemo]; ok {
				if idx.dirGraphs[trimmedSysMemo].IsParentDescendant(txnTo, txnFrom) {
					//prevent cycle
					continue
				}

				balances[txnTo] += incrementBy
			}

			if idx.dirBalances[trimmedSysMemo][txnFrom] > 0 {
				idx.dirGraphs[trimmedSysMemo].Link(txnFrom, txnTo, float64(incrementBy), block.Header.Height, txn.Time)
				idx.dirBalances[trimmedSysMemo][txnFrom] -= incrementBy
			} else {
				idx.dirGraphs[trimmedSysMemo].Link(pad44("0"), txnTo, float64(incrementBy), block.Header.Height, txn.Time)
			}

		} else {

			/*
				Build directory graph.
			*/
			nodesOk, dirlbl, nodes, revision := inflateNodes(txnTo)
			var directoryGraph *Graph
			var dirBalances map[string]int64

			for key, lbl := range idx.directories {
				if lbl == dirlbl {
					//TODO: handle directorylabel collisions
					directoryGraph = idx.dirGraphs[key]
					dirBalances = idx.dirBalances[key]
				}
			}

			if dirBalances[txnFrom] < incrementBy {
				//insufficient balance; skip transaction
				continue
			}

			if nodesOk && directoryGraph != nil {				
				directoryGraph.Link(txnFrom, txnTo, float64(incrementBy), block.Header.Height, txn.Time)
				dirBalances[txnFrom] -= incrementBy

				if _, ok := idx.keyState[pad44(txnTo)]; !ok {
					idx.keyState[pad44(txnTo)] = &KeyState{}
				}

				idx.keyState[pad44(txnTo)].time = txn.Time
				idx.keyState[pad44(txnTo)].revision = revision
				idx.keyState[pad44(txnTo)].label = nodes[len(nodes)-1]
				idx.keyState[pad44(txnTo)].memo = txn.Memo

				timestamp := time.Unix(txn.Time, 0)
				YEAR := timestamp.UTC().Format("2006")
				MONTH := timestamp.UTC().Format("2006+01")
				DAY := timestamp.UTC().Format("2006+01+02")

				DIMENSION_WEIGHT := float64(incrementBy / 4)

				/*
					1/4 temporal
					(stagger timing: +20)
				*/
				directoryGraph.Link(txnTo, DAY, DIMENSION_WEIGHT, block.Header.Height, txn.Time+20)
				directoryGraph.Link(DAY, MONTH, DIMENSION_WEIGHT, block.Header.Height, txn.Time+21)
				directoryGraph.Link(MONTH, YEAR, DIMENSION_WEIGHT, block.Header.Height, txn.Time+22)
				directoryGraph.Link(YEAR, "0", DIMENSION_WEIGHT, block.Header.Height, txn.Time+23)

				/*
					1/4 revision
					(stagger timing: +30)
				*/
				revisionNode := "+" + strconv.Itoa(int(revision))
				directoryGraph.Link(txnTo, revisionNode, DIMENSION_WEIGHT, block.Header.Height, txn.Time+30)
				directoryGraph.Link(revisionNode, "0", DIMENSION_WEIGHT, block.Header.Height, txn.Time+31)

				/*
					1/4 spatial
					(stagger timing: +40)
				*/
				reversedNodes := reverse(nodes)

				//fractionalWeight := DIMENSION_WEIGHT / float64(len(reversedNodes))

				for i := 0; i < len(reversedNodes); i++ {
					node := reversedNodes[i]
					additive := 40 + int64(i)
					
					if i == 0 {
						directoryGraph.Link(txnTo, node, DIMENSION_WEIGHT, block.Header.Height, txn.Time+additive)
					}

					if j := i + 1; j < len(reversedNodes) {
						next := reversedNodes[j]
						directoryGraph.Link(node, next, DIMENSION_WEIGHT, block.Header.Height, txn.Time+additive+int64(j)) // => accumulated
					}

					if i == len(reversedNodes)-1 { //last node => root
						directoryGraph.Link(node, "0", DIMENSION_WEIGHT, block.Header.Height, txn.Time+additive+int64(i+1)) // => total spatial accumulation
					}
				}

				/*
					1/4 periodic
					(stagger timing: +10)
				*/
				blockHeight := strconv.FormatInt(block.Header.Height, 10)
				directoryGraph.Link(txnTo, blockHeight, DIMENSION_WEIGHT, block.Header.Height, txn.Time+10)

				orders := DiminishingOrders(block.Header.Height)

				for j := 1; j < len(orders); j++ {
					i := j - 1

					source := strconv.FormatInt(orders[i], 10)
					target := strconv.FormatInt(orders[j], 10)

					directoryGraph.Link(source, target, DIMENSION_WEIGHT, block.Header.Height, txn.Time+10+int64(j))
				}
			}
		}
	}
}

func (idx *Indexer) rankGraph() {
	log.Printf("Indexer ranking %d directories at height: %d\n", len(idx.dirGraphs), idx.latestHeight)

	for _, cnGraph := range idx.dirGraphs {
		cnGraph.Rank(1.0, 1e-6)
	}

	log.Printf("Finished Ranking %d directories", len(idx.dirGraphs))
}

// Shutdown stops the indexer synchronously.
func (idx *Indexer) Shutdown() {
	close(idx.shutdownChan)
	idx.wg.Wait()
	log.Printf("Indexer shutdown\n")
}
