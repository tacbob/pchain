package state

import (
	"bytes"
	"io/ioutil"
	//"sync"
	"time"

	. "github.com/tendermint/go-common"
	//cfg "github.com/tendermint/go-config"
	//dbm "github.com/tendermint/go-db"
	"github.com/tendermint/go-wire"
	//"github.com/ethereum/go-ethereum/consensus/tendermint/state/txindex"
	//"github.com/ethereum/go-ethereum/consensus/tendermint/state/txindex/null"
	"github.com/ethereum/go-ethereum/consensus/tendermint/types"
	//"fmt"
	"github.com/ethereum/go-ethereum/consensus/tendermint/epoch"
	"github.com/pkg/errors"
)

/*
var (
	stateKey         = []byte("stateKey")
)
*/
//-----------------------------------------------------------------------------

// NOTE: not goroutine-safe.
type State struct {
	// mtx for writing to db
	//mtx sync.Mutex
	//db  dbm.DB

	// should not change
	GenesisDoc *types.GenesisDoc

	/*
	ChainID    string
	Height     uint64 // Genesis state has this set to 0.  So, Block(H=0) does not exist.
	Time       time.Time
	BlockID    types.BlockID
	NeedToSave 	bool //record the number of the block which should be saved to main chain
	EpochNumber	uint64
	*/
	TdmExtra *types.TendermintExtra

	Epoch *epoch.Epoch
	//Validators      *types.ValidatorSet
	//LastValidators  *types.ValidatorSet // block.LastCommit validated against this

	// AppHash is updated after Commit
	//AppHash []byte

	//TxIndexer txindex.TxIndexer `json:"-"` // Transaction indexer.

	// Intermediate results from processing
	// Persisted separately from the state
	//abciResponses *ABCIResponses
}
/*
func LoadState(stateDB dbm.DB) *State {
	state := loadState(stateDB, stateKey)
	return state
}

func loadState(db dbm.DB, key []byte) *State {
	s := &State{db: db, TxIndexer: &null.TxIndex{}}
	buf := db.Get(key)
	if len(buf) == 0 {
		return nil
	} else {
		r, n, err := bytes.NewReader(buf), new(int), new(error)
		wire.ReadBinaryPtr(&s, r, 0, n, err)
		if *err != nil {
			// DATA HAS BEEN CORRUPTED OR THE SPEC HAS CHANGED
			Exit(Fmt("LoadState: Data has been corrupted or its spec has changed: %v\n", *err))
		}
		// TODO: ensure that buf is completely read.
	}
	return s
}
*/

func (s *State) Copy() *State {
	//fmt.Printf("State.Copy(), s.LastValidators are %v\n",s.LastValidators)
	//debug.PrintStack()

	return &State{
		//db:              s.db,
		GenesisDoc:      s.GenesisDoc,
		/*
		ChainID:         s.ChainID,
		Height:			 s.Height,
		BlockID:         s.BlockID,
		Time: 		     s.Time,
		EpochNumber:     s.EpochNumber,
		NeedToSave:      s.NeedToSave,
		*/
		TdmExtra:        s.TdmExtra.Copy(),
		Epoch:           s.Epoch.Copy(),
		//Validators:      s.Validators.Copy(),
		//LastValidators:  s.LastValidators.Copy(),
		//AppHash:         s.AppHash,
		//TxIndexer:       s.TxIndexer, // pointer here, not value
	}
}
/*
func (s *State) Save() {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.db.SetSync(stateKey, s.Bytes())
}
*/
func (s *State) Equals(s2 *State) bool {
	return bytes.Equal(s.Bytes(), s2.Bytes())
}

func (s *State) Bytes() []byte {
	buf, n, err := new(bytes.Buffer), new(int), new(error)
	wire.WriteBinary(s, buf, n, err)
	if *err != nil {
		PanicCrisis(*err)
	}
	return buf.Bytes()
}

// Mutate state variables to match block and validators
// after running EndBlock
func (s *State) SetBlockAndEpoch(tdmExtra *types.TendermintExtra, blockPartsHeader types.PartSetHeader) {

	s.setBlockAndEpoch(tdmExtra.Height, types.BlockID{tdmExtra.Hash(), blockPartsHeader}, tdmExtra.Time)
}

func (s *State) setBlockAndEpoch(
	height uint64, blockID types.BlockID, blockTime time.Time) {

	s.TdmExtra.Height = height
	s.TdmExtra.Time = blockTime
	s.TdmExtra.EpochNumber = uint64(s.Epoch.Number)
	//s.Validators = nextValSet
	//s.LastValidators = prevValSet
}

func (s *State) GetValidators() (*types.ValidatorSet, *types.ValidatorSet, error) {

	if s.Epoch == nil {
		return nil, nil, errors.New("epoch does not exist")
	}

	if s.TdmExtra.EpochNumber == uint64(s.Epoch.Number) {
		return s.Epoch.Validators, s.Epoch.Validators, nil
	} else if s.TdmExtra.EpochNumber == uint64(s.Epoch.Number - 1) {
		return s.Epoch.PreviousEpoch.Validators, s.Epoch.Validators, nil
	}

	return nil, nil, errors.New("epoch information error")
}
/*
// Load the most recent state from "state" db,
// or create a new one (and save) from genesis.
func GetState(config cfg.Config, stateDB dbm.DB) *State {
	state := LoadState(stateDB)
	if state == nil {
		state = MakeGenesisStateFromFile(stateDB, config.GetString("genesis_file"))

		state.Save()

		_, val, _ := state.GetValidators()
		fmt.Printf("GetState() state 0, state.validators are: %v\n", val)
	} else if valSetFromGenesis {
		valSet := MakeGenesisValidatorsFromFile(config.GetString("genesis_file"))
		state.Validators = valSet
		state.Save()

		_, val := state.GetValidators()
		fmt.Printf("GetState() state 1, state.validators are: %v\n", val)
	} else {
		_, val, _ := state.GetValidators()
		fmt.Printf("GetState() state 2, state.validators are: %v\n", val)
	}
	return state
}
*/

//-----------------------------------------------------------------------------
// Genesis

// MakeGenesisStateFromFile reads and unmarshals state from the given file.
//
// Used during replay and in tests.
func MakeGenesisStateFromFile(/*db dbm.DB, */genDocFile string) *State {
	genDocJSON, err := ioutil.ReadFile(genDocFile)
	if err != nil {
		Exit(Fmt("Couldn't read GenesisDoc file: %v", err))
	}
	genDoc, err := types.GenesisDocFromJSON(genDocJSON)
	if err != nil {
		Exit(Fmt("Error reading GenesisDoc: %v", err))
	}
	return MakeGenesisState(/*db, */genDoc)
}

// MakeGenesisState creates state from types.GenesisDoc.
//
// Used in tests.
func MakeGenesisState(/*db dbm.DB, */genDoc *types.GenesisDoc) *State {
	if len(genDoc.CurrentEpoch.Validators) == 0 {
		Exit(Fmt("The genesis file has no validators"))
	}

	if genDoc.GenesisTime.IsZero() {
		genDoc.GenesisTime = time.Now()
	}

	// Make validators slice
	validators := make([]*types.Validator, len(genDoc.CurrentEpoch.Validators))
	for i, val := range genDoc.CurrentEpoch.Validators {
		pubKey := val.PubKey
		address := pubKey.Address()

		// Make validator
		validators[i] = &types.Validator{
			Address:     address,
			PubKey:      pubKey,
			VotingPower: val.Amount,
		}
	}

	return &State{
		//db:              db,
		GenesisDoc:      genDoc,
		TdmExtra:        &types.TendermintExtra{
			ChainID:     genDoc.ChainID,
			Height:      0,
			Time:        genDoc.GenesisTime,
			EpochNumber: 0,
			NeedToSave:  false,
		},
		//Validators:      types.NewValidatorSet(validators),
		//LastValidators:  types.NewValidatorSet(nil),
		//AppHash:         genDoc.AppHash,
		//TxIndexer:       &null.TxIndex{}, // we do not need indexer during replay and in tests
	}
}

func MakeGenesisValidatorsFromFile(genDocFile string) *types.ValidatorSet {

	genDocJSON, err := ioutil.ReadFile(genDocFile)
	if err != nil {
		Exit(Fmt("MakeGenesisValidatorsFromFile(), Couldn't read GenesisDoc file: %v", err))
	}

	genDoc, err := types.GenesisDocFromJSON(genDocJSON)
	if err != nil {
		Exit(Fmt("MakeGenesisValidatorsFromFile(), Error reading GenesisDoc: %v", err))
	}

	if len(genDoc.CurrentEpoch.Validators) == 0 {
		Exit(Fmt("MakeGenesisValidatorsFromFile(), The genesis file has no validators"))
	}

	// Make validators slice
	validators := make([]*types.Validator, len(genDoc.CurrentEpoch.Validators))
	for i, val := range genDoc.CurrentEpoch.Validators {
		pubKey := val.PubKey
		address := pubKey.Address()

		// Make validator
		validators[i] = &types.Validator{
			Address:     address,
			PubKey:      pubKey,
			VotingPower: val.Amount,
		}
	}

	return types.NewValidatorSet(validators)
}