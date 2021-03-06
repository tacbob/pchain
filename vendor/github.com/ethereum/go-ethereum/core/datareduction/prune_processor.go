package datareduction

import (
	"bytes"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"log"
	"sort"
)

var (
	// max scan trie height
	max_count_trie uint64 = 100
	// max retain trie height
	max_remain_trie uint64 = 10
	// emptyRoot is the known root hash of an empty trie.
	emptyRoot = common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
)

type PruneProcessor struct {
	db      ethdb.Database // Low level persistent database to store prune counting statistics
	prunedb PruneDatabase

	bc      *core.BlockChain
	chainDb ethdb.Database // database instance to delete the state/block data

	pendingDeleteHashList []common.Hash // Temp List for batch delete the node
}

type NodeCount map[common.Hash]uint64

type processLeafTrie func(addrHash common.Hash, account *state.Account)

func NewPruneProcessor(chaindb, prunedb ethdb.Database, bc *core.BlockChain) *PruneProcessor {
	return &PruneProcessor{
		db:                    prunedb,
		prunedb:               NewDatabase(prunedb),
		bc:                    bc,
		chainDb:               chaindb,
		pendingDeleteHashList: nil,
	}
}

func (p *PruneProcessor) Process(blockNumber, scanNumber, pruneNumber uint64) (uint64, uint64) {
	newScanNumber := scanNumber
	newPruneNumber := pruneNumber

	// Step 1. determine the scan height
	scanOrNot, scanStart, scanEnd := calculateScan(scanNumber, blockNumber)
	log.Println("Step 1.", scanOrNot, scanStart, scanEnd)

	if scanOrNot {
		// Step 2. Read Latest Node Count
		nodeCount := p.readLatestNodeCount(scanNumber, pruneNumber)

		var stateRoots []common.Hash

		for i := scanStart; i <= scanEnd; i++ {

			//PrintMemUsage()
			//start := time.Now()

			//TODO Cache the header

			header := p.bc.GetHeaderByNumber(i)

			//log.Printf("Block: %v, Root %x", i, header.Root)
			p.countBlockChainTrie(header.Root, nodeCount)
			stateRoots = append(stateRoots, header.Root)
			//log.Println(nodeCount)
			//PrintMemUsage()

			// Prune Data Process
			if len(nodeCount) > 0 && len(stateRoots) >= int(max_count_trie) {
				newScanNumber = i
				newPruneNumber = p.processScanData(newScanNumber, stateRoots, nodeCount)
				// Remain state root
				stateRoots = stateRoots[len(stateRoots)-int(max_remain_trie):]
			}
		}

		// Process the remaining state root
		if len(nodeCount) > 0 {
			newScanNumber = scanEnd
			newPruneNumber = p.processScanData(newScanNumber, stateRoots, nodeCount)
		}
	}
	return newScanNumber, newPruneNumber
}

func calculateScan(scan, latestBlockHeight uint64) (scanOrNot bool, from, to uint64) {

	var unscanHeight uint64
	if scan == 0 {
		unscanHeight = latestBlockHeight
		from = 0
	} else {
		unscanHeight = latestBlockHeight - scan
		from = scan + 1
	}

	if unscanHeight > max_count_trie {
		mul := latestBlockHeight / max_count_trie
		to = mul * max_count_trie
	}

	if to != 0 {
		scanOrNot = true
	}

	return
}

func (p *PruneProcessor) readLatestNodeCount(scanNumber, pruneNumber uint64) NodeCount {
	nodeCount := make(NodeCount)

	lastHash := rawdb.ReadDataPruneTrieRootHash(p.db, scanNumber, pruneNumber)
	log.Println("Last Hash: ", lastHash.Hex())
	if (lastHash != common.Hash{}) {
		lastPruneTrie, openErr := p.prunedb.OpenPruneTrie(lastHash)
		if openErr != nil {
			log.Println("Unable read the last Prune Trie. Error ", openErr)
		} else {
			it := trie.NewIterator(lastPruneTrie.NodeIterator(nil))
			for it.Next() {
				nodeHash := common.BytesToHash(lastPruneTrie.GetKey(it.Key))
				var nodeHashCount uint64
				rlp.DecodeBytes(it.Value, &nodeHashCount)
				nodeCount[nodeHash] = nodeHashCount
			}
		}
	}
	return nodeCount
}

func (p *PruneProcessor) countBlockChainTrie(root common.Hash, nodeCount NodeCount) {
	t, openErr := p.bc.StateCache().OpenTrie(root)
	if openErr != nil {
		log.Println(openErr)
		return
	}

	countTrie(t, nodeCount, func(addrHash common.Hash, account *state.Account) {
		if account.Root != emptyRoot {
			if storageTrie, stErr := p.bc.StateCache().OpenStorageTrie(addrHash, account.Root); stErr == nil {
				countTrie(storageTrie, nodeCount, nil)
			}
		}

		if account.TX1Root != emptyRoot {
			if tx1Trie, tx1Err := p.bc.StateCache().OpenTX1Trie(addrHash, account.TX1Root); tx1Err == nil {
				countTrie(tx1Trie, nodeCount, nil)
			}
		}

		if account.TX3Root != emptyRoot {
			if tx3Trie, tx3Err := p.bc.StateCache().OpenTX3Trie(addrHash, account.TX3Root); tx3Err == nil {
				countTrie(tx3Trie, nodeCount, nil)
			}
		}

		if account.ProxiedRoot != emptyRoot {
			if proxiedTrie, proxiedErr := p.bc.StateCache().OpenProxiedTrie(addrHash, account.ProxiedRoot); proxiedErr == nil {
				countTrie(proxiedTrie, nodeCount, nil)
			}
		}

		if account.RewardRoot != emptyRoot {
			if rewardTrie, rewardErr := p.bc.StateCache().OpenRewardTrie(addrHash, account.RewardRoot); rewardErr == nil {
				countTrie(rewardTrie, nodeCount, nil)
			}
		}

	})
}

func countTrie(t state.Trie, nodeCount NodeCount, processLeaf processLeafTrie) {
	it := t.NodeIterator(nil)
	child := true
	for i := 0; it.Next(child); i++ {
		if !it.Leaf() {
			nodeHash := it.Hash()
			if _, exist := nodeCount[nodeHash]; exist {
				nodeCount[nodeHash]++
				child = false
			} else {
				nodeCount[nodeHash] = 1
				child = true
			}
		} else {
			// Process the Account -> Inner Trie
			if processLeaf != nil {
				addr := t.GetKey(it.LeafKey())
				if len(addr) == 20 {
					addrHash := common.BytesToHash(it.LeafKey())

					var data state.Account
					rlp.DecodeBytes(it.LeafBlob(), &data)

					processLeaf(addrHash, &data)
				}
			}
		}
	}
}

func (p *PruneProcessor) processScanData(latestScanNumber uint64, stateRoots []common.Hash, nodeCount NodeCount) uint64 {
	log.Println("After Scan, Total Nodes: ", latestScanNumber, len(nodeCount))

	// Prune State Data
	p.pruneData(stateRoots[:len(stateRoots)-int(max_remain_trie)], nodeCount)

	newPruneNumber := latestScanNumber - max_remain_trie

	// Commit the new scaned/pruned node count to trie
	p.commitDataPruneTrie(nodeCount, latestScanNumber, newPruneNumber)

	log.Println("After Prune, Total Nodes: ", len(nodeCount))
	log.Println("Scan/Prune Completed for trie", latestScanNumber, newPruneNumber)
	return newPruneNumber
}

func (p *PruneProcessor) pruneData(stateRoots []common.Hash, nodeCount NodeCount) {
	for _, root := range stateRoots {
		p.pruneBlockChainTrie(root, nodeCount)
	}

	batch := p.chainDb.NewBatch()
	for _, hash := range p.pendingDeleteHashList {
		batch.Delete(hash.Bytes())
	}
	writeErr := batch.Write()
	log.Println(writeErr)
	p.pendingDeleteHashList = nil
}

func (p *PruneProcessor) pruneBlockChainTrie(root common.Hash, nodeCount NodeCount) {
	t, openErr := p.bc.StateCache().OpenTrie(root)
	if openErr != nil {
		log.Println(openErr)
		return
	}

	pruneTrie(t, nodeCount, &p.pendingDeleteHashList, func(addrHash common.Hash, account *state.Account) {
		if account.Root != emptyRoot {
			if storageTrie, stErr := p.bc.StateCache().OpenStorageTrie(addrHash, account.Root); stErr == nil {
				pruneTrie(storageTrie, nodeCount, &p.pendingDeleteHashList, nil)
			}
		}

		if account.TX1Root != emptyRoot {
			if tx1Trie, tx1Err := p.bc.StateCache().OpenTX1Trie(addrHash, account.TX1Root); tx1Err == nil {
				pruneTrie(tx1Trie, nodeCount, &p.pendingDeleteHashList, nil)
			}
		}

		if account.TX3Root != emptyRoot {
			if tx3Trie, tx3Err := p.bc.StateCache().OpenTX3Trie(addrHash, account.TX3Root); tx3Err == nil {
				pruneTrie(tx3Trie, nodeCount, &p.pendingDeleteHashList, nil)
			}
		}

		if account.ProxiedRoot != emptyRoot {
			if proxiedTrie, proxiedErr := p.bc.StateCache().OpenProxiedTrie(addrHash, account.ProxiedRoot); proxiedErr == nil {
				pruneTrie(proxiedTrie, nodeCount, &p.pendingDeleteHashList, nil)
			}
		}

		if account.RewardRoot != emptyRoot {
			if rewardTrie, rewardErr := p.bc.StateCache().OpenRewardTrie(addrHash, account.RewardRoot); rewardErr == nil {
				pruneTrie(rewardTrie, nodeCount, &p.pendingDeleteHashList, nil)
			}
		}
	})

}

func pruneTrie(t state.Trie, nodeCount NodeCount, pendingDeleteHashList *[]common.Hash, processLeaf processLeafTrie) {
	it := t.NodeIterator(nil)
	child := true
	for i := 0; it.Next(child); i++ {
		if !it.Leaf() {
			nodeHash := it.Hash()
			if nodeCount[nodeHash] > 0 {
				nodeCount[nodeHash]--
			}

			if nodeCount[nodeHash] == 0 {
				child = true
				*pendingDeleteHashList = append(*pendingDeleteHashList, nodeHash)
				delete(nodeCount, nodeHash)
			} else {
				child = false
			}
		} else {
			// Process the Account -> Inner Trie
			if processLeaf != nil {
				addr := t.GetKey(it.LeafKey())
				if len(addr) == 20 {
					addrHash := common.BytesToHash(it.LeafKey())

					var data state.Account
					rlp.DecodeBytes(it.LeafBlob(), &data)

					processLeaf(addrHash, &data)
				}
			}
		}
	}
}

func (p *PruneProcessor) commitDataPruneTrie(nodeCount NodeCount, lastScanNumber, lastPruneNumber uint64) {
	// Store the Node Count into data prune trie
	// Commit the Prune Trie
	pruneTrie, _ := p.prunedb.OpenPruneTrie(common.Hash{})

	for key, count := range nodeCount {
		value, _ := rlp.EncodeToBytes(count)
		pruneTrie.TryUpdate(key[:], value)
	}
	pruneTrieRoot, commit_err := pruneTrie.Commit(nil)
	log.Println("Commit Hash", pruneTrieRoot.Hex(), commit_err)
	// Commit to Prune DB
	db_commit_err := p.prunedb.TrieDB().Commit(pruneTrieRoot, true)
	log.Println(db_commit_err)

	// Write the Root Hash of Prune Trie
	rawdb.WriteDataPruneTrieRootHash(p.db, pruneTrieRoot, lastScanNumber, lastPruneNumber)
	rawdb.WriteHeadScanNumber(p.db, lastScanNumber)
	rawdb.WriteHeadPruneNumber(p.db, lastPruneNumber)
}

func (nc NodeCount) String() string {
	list := make([]common.Hash, 0, len(nc))
	for key := range nc {
		list = append(list, key)
	}
	sort.Slice(list, func(i, j int) bool {
		return bytes.Compare(list[i].Bytes(), list[j].Bytes()) == 1
	})

	result := ""
	for _, key := range list {
		result += fmt.Sprintf("%v: %d \n", key.Hex(), nc[key])
	}
	return result
}
