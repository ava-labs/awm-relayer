// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package batchcrosschainmessenger

import (
	"errors"
	"math/big"
	"strings"

	"github.com/ava-labs/subnet-evm/accounts/abi"
	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/interfaces"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = interfaces.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// BatchCrossChainMessengerMetaData contains all meta data concerning the BatchCrossChainMessenger contract.
var BatchCrossChainMessengerMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"teleporterRegistryAddress\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"teleporterManager\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"oldMinTeleporterVersion\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"newMinTeleporterVersion\",\"type\":\"uint256\"}],\"name\":\"MinTeleporterVersionUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferred\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"sourceBlockchainID\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"originSenderAddress\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"string[]\",\"name\":\"messages\",\"type\":\"string[]\"}],\"name\":\"ReceiveMessages\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"destinationBlockchainID\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"destinationAddress\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"feeTokenAddress\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"feeAmount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"requiredGasLimit\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"string[]\",\"name\":\"messages\",\"type\":\"string[]\"}],\"name\":\"SendMessages\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"teleporterAddress\",\"type\":\"address\"}],\"name\":\"TeleporterAddressPaused\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"teleporterAddress\",\"type\":\"address\"}],\"name\":\"TeleporterAddressUnpaused\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"sourceBlockchainID\",\"type\":\"bytes32\"}],\"name\":\"getCurrentMessages\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"},{\"internalType\":\"string[]\",\"name\":\"\",\"type\":\"string[]\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getMinTeleporterVersion\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"teleporterAddress\",\"type\":\"address\"}],\"name\":\"isTeleporterAddressPaused\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"teleporterAddress\",\"type\":\"address\"}],\"name\":\"pauseTeleporterAddress\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"sourceBlockchainID\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"originSenderAddress\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"}],\"name\":\"receiveTeleporterMessage\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"renounceOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"destinationBlockchainID\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"destinationAddress\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"feeTokenAddress\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"feeAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"requiredGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"string[]\",\"name\":\"messages\",\"type\":\"string[]\"}],\"name\":\"sendMessages\",\"outputs\":[{\"internalType\":\"bytes32[]\",\"name\":\"\",\"type\":\"bytes32[]\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"teleporterRegistry\",\"outputs\":[{\"internalType\":\"contractTeleporterRegistry\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"transferOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"teleporterAddress\",\"type\":\"address\"}],\"name\":\"unpauseTeleporterAddress\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"version\",\"type\":\"uint256\"}],\"name\":\"updateMinTeleporterVersion\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x60a06040523480156200001157600080fd5b50604051620020403803806200204083398101604081905262000034916200029f565b60016000558181816001600160a01b038116620000be5760405162461bcd60e51b815260206004820152603760248201527f54656c65706f727465725570677261646561626c653a207a65726f2074656c6560448201527f706f72746572207265676973747279206164647265737300000000000000000060648201526084015b60405180910390fd5b6001600160a01b03811660808190526040805163301fd1f560e21b8152905163c07f47d4916004808201926020929091908290030181865afa15801562000109573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906200012f9190620002d7565b600255506200013e3362000153565b6200014981620001a5565b50505050620002f1565b600380546001600160a01b038381166001600160a01b0319831681179093556040519116919082907f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e090600090a35050565b620001af62000224565b6001600160a01b038116620002165760405162461bcd60e51b815260206004820152602660248201527f4f776e61626c653a206e6577206f776e657220697320746865207a65726f206160448201526564647265737360d01b6064820152608401620000b5565b620002218162000153565b50565b6003546001600160a01b03163314620002805760405162461bcd60e51b815260206004820181905260248201527f4f776e61626c653a2063616c6c6572206973206e6f7420746865206f776e65726044820152606401620000b5565b565b80516001600160a01b03811681146200029a57600080fd5b919050565b60008060408385031215620002b357600080fd5b620002be8362000282565b9150620002ce6020840162000282565b90509250929050565b600060208284031215620002ea57600080fd5b5051919050565b608051611d1f620003216000396000818160be0152818161073701528181610c570152610fce0152611d1f6000f3fe608060405234801561001057600080fd5b50600436106100b45760003560e01c80638da5cb5b116100715780638da5cb5b146101605780639731429714610171578063c1329fcb146101ad578063c868efaa146101ce578063d2cc7a70146101e1578063f2fde38b146101f257600080fd5b80631a7f5bec146100b95780632b0d8f18146100fd5780633902970c146101125780634511243e146101325780635eb9951414610145578063715018a614610158575b600080fd5b6100e07f000000000000000000000000000000000000000000000000000000000000000081565b6040516001600160a01b0390911681526020015b60405180910390f35b61011061010b3660046114c8565b610205565b005b610125610120366004611578565b61030a565b6040516100f491906116c3565b6101106101403660046114c8565b6104e8565b610110610153366004611707565b6105e5565b6101106105f9565b6003546001600160a01b03166100e0565b61019d61017f3660046114c8565b6001600160a01b031660009081526001602052604090205460ff1690565b60405190151581526020016100f4565b6101c06101bb366004611707565b61060d565b6040516100f49291906117c5565b6101106101dc3660046117e9565b610722565b6002546040519081526020016100f4565b6101106102003660046114c8565b6108ec565b61020d610962565b6001600160a01b03811661023c5760405162461bcd60e51b815260040161023390611872565b60405180910390fd5b6001600160a01b03811660009081526001602052604090205460ff16156102bb5760405162461bcd60e51b815260206004820152602d60248201527f54656c65706f727465725570677261646561626c653a2061646472657373206160448201526c1b1c9958591e481c185d5cd959609a1b6064820152608401610233565b6001600160a01b0381166000818152600160208190526040808320805460ff1916909217909155517f933f93e57a222e6330362af8b376d0a8725b6901e9a2fb86d00f169702b28a4c9190a250565b606061031461096a565b600084156103295761032686866109c3565b90505b866001600160a01b0316887f430d1906813fdb2129a19139f4112a1396804605501a798df3a4042590ba20d58884888860405161036994939291906118c0565b60405180910390a36000835167ffffffffffffffff81111561038d5761038d6114e5565b6040519080825280602002602001820160405280156103b6578160200160208202803683370190505b50905060005b84518110156104d057600061049d6040518060c001604052808d81526020018c6001600160a01b0316815260200160405180604001604052808d6001600160a01b03168152602001888152508152602001898152602001600067ffffffffffffffff81111561042d5761042d6114e5565b604051908082528060200260200182016040528015610456578160200160208202803683370190505b50815260200188858151811061046e5761046e6118ed565b60200260200101516040516020016104869190611903565b604051602081830303815290604052815250610b2d565b9050808383815181106104b2576104b26118ed565b602090810291909101015250806104c88161192c565b9150506103bc565b509150506104de6001600055565b9695505050505050565b6104f0610962565b6001600160a01b0381166105165760405162461bcd60e51b815260040161023390611872565b6001600160a01b03811660009081526001602052604090205460ff166105905760405162461bcd60e51b815260206004820152602960248201527f54656c65706f727465725570677261646561626c653a2061646472657373206e6044820152681bdd081c185d5cd95960ba1b6064820152608401610233565b6040516001600160a01b038216907f844e2f3154214672229235858fd029d1dfd543901c6d05931f0bc2480a2d72c390600090a26001600160a01b03166000908152600160205260409020805460ff19169055565b6105ed610962565b6105f681610c53565b50565b610601610df3565b61060b6000610e4d565b565b60008181526004602090815260408083208151808301835281546001600160a01b0316815260018201805484518187028101870190955280855260609587959394938582019390929091879084015b8282101561070857838290600052602060002001805461067b90611945565b80601f01602080910402602001604051908101604052809291908181526020018280546106a790611945565b80156106f45780601f106106c9576101008083540402835291602001916106f4565b820191906000526020600020905b8154815290600101906020018083116106d757829003601f168201915b50505050508152602001906001019061065c565b505050915250508051602090910151909590945092505050565b61072a61096a565b6002546001600160a01b037f000000000000000000000000000000000000000000000000000000000000000016634c1f08ce336040516001600160e01b031960e084901b1681526001600160a01b039091166004820152602401602060405180830381865afa1580156107a1573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906107c5919061197f565b101561082c5760405162461bcd60e51b815260206004820152603060248201527f54656c65706f727465725570677261646561626c653a20696e76616c6964205460448201526f32b632b837b93a32b91039b2b73232b960811b6064820152608401610233565b6108353361017f565b1561089b5760405162461bcd60e51b815260206004820152603060248201527f54656c65706f727465725570677261646561626c653a2054656c65706f72746560448201526f1c881859191c995cdcc81c185d5cd95960821b6064820152608401610233565b6108dc848484848080601f016020809104026020016040519081016040528093929190818152602001838380828437600092019190915250610e9f92505050565b6108e66001600055565b50505050565b6108f4610df3565b6001600160a01b0381166109595760405162461bcd60e51b815260206004820152602660248201527f4f776e61626c653a206e6577206f776e657220697320746865207a65726f206160448201526564647265737360d01b6064820152608401610233565b6105f681610e4d565b61060b610df3565b6002600054036109bc5760405162461bcd60e51b815260206004820152601f60248201527f5265656e7472616e637947756172643a207265656e7472616e742063616c6c006044820152606401610233565b6002600055565b6040516370a0823160e01b815230600482015260009081906001600160a01b038516906370a0823190602401602060405180830381865afa158015610a0c573d6000803e3d6000fd5b505050506040513d601f19601f82011682018060405250810190610a30919061197f565b9050610a476001600160a01b038516333086610f5e565b6040516370a0823160e01b81523060048201526000906001600160a01b038616906370a0823190602401602060405180830381865afa158015610a8e573d6000803e3d6000fd5b505050506040513d601f19601f82011682018060405250810190610ab2919061197f565b9050818111610b185760405162461bcd60e51b815260206004820152602c60248201527f5361666545524332305472616e7366657246726f6d3a2062616c616e6365206e60448201526b1bdd081a5b98dc99585cd95960a21b6064820152608401610233565b610b228282611998565b925050505b92915050565b600080610b38610fc9565b60408401516020015190915015610bdd576040830151516001600160a01b0316610bba5760405162461bcd60e51b815260206004820152602d60248201527f54656c65706f727465725570677261646561626c653a207a65726f206665652060448201526c746f6b656e206164647265737360981b6064820152608401610233565b604083015160208101519051610bdd916001600160a01b039091169083906110dd565b604051630624488560e41b81526001600160a01b03821690636244885090610c099086906004016119ef565b6020604051808303816000875af1158015610c28573d6000803e3d6000fd5b505050506040513d601f19601f82011682018060405250810190610c4c919061197f565b9392505050565b60007f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031663c07f47d46040518163ffffffff1660e01b8152600401602060405180830381865afa158015610cb3573d6000803e3d6000fd5b505050506040513d601f19601f82011682018060405250810190610cd7919061197f565b60025490915081831115610d475760405162461bcd60e51b815260206004820152603160248201527f54656c65706f727465725570677261646561626c653a20696e76616c6964205460448201527032b632b837b93a32b9103b32b939b4b7b760791b6064820152608401610233565b808311610dbc5760405162461bcd60e51b815260206004820152603f60248201527f54656c65706f727465725570677261646561626c653a206e6f7420677265617460448201527f6572207468616e2063757272656e74206d696e696d756d2076657273696f6e006064820152608401610233565b6002839055604051839082907fa9a7ef57e41f05b4c15480842f5f0c27edfcbb553fed281f7c4068452cc1c02d90600090a3505050565b6003546001600160a01b0316331461060b5760405162461bcd60e51b815260206004820181905260248201527f4f776e61626c653a2063616c6c6572206973206e6f7420746865206f776e65726044820152606401610233565b600380546001600160a01b038381166001600160a01b0319831681179093556040519116919082907f8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e090600090a35050565b600081806020019051810190610eb59190611a6d565b6040805180820182526001600160a01b038681168252602080830185815260008a81526004835294909420835181546001600160a01b03191693169290921782559251805194955091939092610f129260018501929101906113f6565b50905050826001600160a01b0316847f903a123a8d947dcbd005d05b20bb5180f878c6d3527681c1abb1d8987714cb9d83604051610f509190611b5a565b60405180910390a350505050565b6040516001600160a01b03808516602483015283166044820152606481018290526108e69085906323b872dd60e01b906084015b60408051601f198184030181529190526020810180516001600160e01b03166001600160e01b03199093169290921790915261118f565b6000807f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031663d820e64f6040518163ffffffff1660e01b8152600401602060405180830381865afa15801561102a573d6000803e3d6000fd5b505050506040513d601f19601f8201168201806040525081019061104e9190611b6d565b9050611072816001600160a01b031660009081526001602052604090205460ff1690565b156110d85760405162461bcd60e51b815260206004820152603060248201527f54656c65706f727465725570677261646561626c653a2054656c65706f72746560448201526f1c881cd95b991a5b99c81c185d5cd95960821b6064820152608401610233565b919050565b604051636eb1769f60e11b81523060048201526001600160a01b038381166024830152600091839186169063dd62ed3e90604401602060405180830381865afa15801561112e573d6000803e3d6000fd5b505050506040513d601f19601f82011682018060405250810190611152919061197f565b61115c9190611b8a565b6040516001600160a01b0385166024820152604481018290529091506108e690859063095ea7b360e01b90606401610f92565b60006111e4826040518060400160405280602081526020017f5361666545524332303a206c6f772d6c6576656c2063616c6c206661696c6564815250856001600160a01b03166112669092919063ffffffff16565b80519091501561126157808060200190518101906112029190611b9d565b6112615760405162461bcd60e51b815260206004820152602a60248201527f5361666545524332303a204552433230206f7065726174696f6e20646964206e6044820152691bdd081cdd58d8d9595960b21b6064820152608401610233565b505050565b6060611275848460008561127d565b949350505050565b6060824710156112de5760405162461bcd60e51b815260206004820152602660248201527f416464726573733a20696e73756666696369656e742062616c616e636520666f6044820152651c8818d85b1b60d21b6064820152608401610233565b600080866001600160a01b031685876040516112fa9190611bbf565b60006040518083038185875af1925050503d8060008114611337576040519150601f19603f3d011682016040523d82523d6000602084013e61133c565b606091505b509150915061134d87838387611358565b979650505050505050565b606083156113c75782516000036113c0576001600160a01b0385163b6113c05760405162461bcd60e51b815260206004820152601d60248201527f416464726573733a2063616c6c20746f206e6f6e2d636f6e74726163740000006044820152606401610233565b5081611275565b61127583838151156113dc5781518083602001fd5b8060405162461bcd60e51b81526004016102339190611903565b82805482825590600052602060002090810192821561143c579160200282015b8281111561143c578251829061142c9082611c29565b5091602001919060010190611416565b5061144892915061144c565b5090565b808211156114485760006114608282611469565b5060010161144c565b50805461147590611945565b6000825580601f10611485575050565b601f0160209004906000526020600020908101906105f691905b80821115611448576000815560010161149f565b6001600160a01b03811681146105f657600080fd5b6000602082840312156114da57600080fd5b8135610c4c816114b3565b634e487b7160e01b600052604160045260246000fd5b604051601f8201601f1916810167ffffffffffffffff81118282101715611524576115246114e5565b604052919050565b600067ffffffffffffffff821115611546576115466114e5565b5060051b60200190565b600067ffffffffffffffff82111561156a5761156a6114e5565b50601f01601f191660200190565b60008060008060008060c0878903121561159157600080fd5b863595506115a260208801356114b3565b602087013594506115b660408801356114b3565b60408701359350606087013592506080870135915067ffffffffffffffff60a088013511156115e457600080fd5b60a0870135870188601f8201126115fa57600080fd5b61160c611607823561152c565b6114fb565b81358082526020808301929160051b8401018b81111561162b57600080fd5b602084015b818110156116b15767ffffffffffffffff8135111561164e57600080fd5b803585018d603f82011261166157600080fd5b602081013561167261160782611550565b8181528f604083850101111561168757600080fd5b81604084016020830137600060208383010152808752505050602084019350602081019050611630565b50508093505050509295509295509295565b6020808252825182820181905260009190848201906040850190845b818110156116fb578351835292840192918401916001016116df565b50909695505050505050565b60006020828403121561171957600080fd5b5035919050565b60005b8381101561173b578181015183820152602001611723565b50506000910152565b6000815180845261175c816020860160208601611720565b601f01601f19169290920160200192915050565b600081518084526020808501808196508360051b8101915082860160005b858110156117b85782840389526117a6848351611744565b9885019893509084019060010161178e565b5091979650505050505050565b6001600160a01b038316815260406020820181905260009061127590830184611770565b600080600080606085870312156117ff57600080fd5b843593506020850135611811816114b3565b9250604085013567ffffffffffffffff8082111561182e57600080fd5b818701915087601f83011261184257600080fd5b81358181111561185157600080fd5b88602082850101111561186357600080fd5b95989497505060200194505050565b6020808252602e908201527f54656c65706f727465725570677261646561626c653a207a65726f2054656c6560408201526d706f72746572206164647265737360901b606082015260800190565b60018060a01b03851681528360208201528260408201526080606082015260006104de6080830184611770565b634e487b7160e01b600052603260045260246000fd5b602081526000610c4c6020830184611744565b634e487b7160e01b600052601160045260246000fd5b60006001820161193e5761193e611916565b5060010190565b600181811c9082168061195957607f821691505b60208210810361197957634e487b7160e01b600052602260045260246000fd5b50919050565b60006020828403121561199157600080fd5b5051919050565b81810381811115610b2757610b27611916565b600081518084526020808501945080840160005b838110156119e45781516001600160a01b0316875295820195908201906001016119bf565b509495945050505050565b60208152815160208201526000602083015160018060a01b03808216604085015260408501519150808251166060850152506020810151608084015250606083015160a0830152608083015160e060c0840152611a506101008401826119ab565b905060a0840151601f198483030160e0850152610b228282611744565b60006020808385031215611a8057600080fd5b825167ffffffffffffffff80821115611a9857600080fd5b818501915085601f830112611aac57600080fd5b8151611aba6116078261152c565b81815260059190911b83018401908481019088831115611ad957600080fd5b8585015b83811015611b4d57805185811115611af55760008081fd5b8601603f81018b13611b075760008081fd5b878101516040611b1961160783611550565b8281528d82848601011115611b2e5760008081fd5b611b3d838c8301848701611720565b8652505050918601918601611add565b5098975050505050505050565b602081526000610c4c6020830184611770565b600060208284031215611b7f57600080fd5b8151610c4c816114b3565b80820180821115610b2757610b27611916565b600060208284031215611baf57600080fd5b81518015158114610c4c57600080fd5b60008251611bd1818460208701611720565b9190910192915050565b601f82111561126157600081815260208120601f850160051c81016020861015611c025750805b601f850160051c820191505b81811015611c2157828155600101611c0e565b505050505050565b815167ffffffffffffffff811115611c4357611c436114e5565b611c5781611c518454611945565b84611bdb565b602080601f831160018114611c8c5760008415611c745750858301515b600019600386901b1c1916600185901b178555611c21565b600085815260208120601f198616915b82811015611cbb57888601518255948401946001909101908401611c9c565b5085821015611cd95787850151600019600388901b60f8161c191681555b5050505050600190811b0190555056fea264697066735822122099c1934ed6c29efc8a8f1a00000c54700422fb3a36d744d19eddd761d3af6f2c64736f6c63430008120033",
}

// BatchCrossChainMessengerABI is the input ABI used to generate the binding from.
// Deprecated: Use BatchCrossChainMessengerMetaData.ABI instead.
var BatchCrossChainMessengerABI = BatchCrossChainMessengerMetaData.ABI

// BatchCrossChainMessengerBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use BatchCrossChainMessengerMetaData.Bin instead.
var BatchCrossChainMessengerBin = BatchCrossChainMessengerMetaData.Bin

// DeployBatchCrossChainMessenger deploys a new Ethereum contract, binding an instance of BatchCrossChainMessenger to it.
func DeployBatchCrossChainMessenger(auth *bind.TransactOpts, backend bind.ContractBackend, teleporterRegistryAddress common.Address, teleporterManager common.Address) (common.Address, *types.Transaction, *BatchCrossChainMessenger, error) {
	parsed, err := BatchCrossChainMessengerMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(BatchCrossChainMessengerBin), backend, teleporterRegistryAddress, teleporterManager)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &BatchCrossChainMessenger{BatchCrossChainMessengerCaller: BatchCrossChainMessengerCaller{contract: contract}, BatchCrossChainMessengerTransactor: BatchCrossChainMessengerTransactor{contract: contract}, BatchCrossChainMessengerFilterer: BatchCrossChainMessengerFilterer{contract: contract}}, nil
}

// BatchCrossChainMessenger is an auto generated Go binding around an Ethereum contract.
type BatchCrossChainMessenger struct {
	BatchCrossChainMessengerCaller     // Read-only binding to the contract
	BatchCrossChainMessengerTransactor // Write-only binding to the contract
	BatchCrossChainMessengerFilterer   // Log filterer for contract events
}

// BatchCrossChainMessengerCaller is an auto generated read-only Go binding around an Ethereum contract.
type BatchCrossChainMessengerCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// BatchCrossChainMessengerTransactor is an auto generated write-only Go binding around an Ethereum contract.
type BatchCrossChainMessengerTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// BatchCrossChainMessengerFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type BatchCrossChainMessengerFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// BatchCrossChainMessengerSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type BatchCrossChainMessengerSession struct {
	Contract     *BatchCrossChainMessenger // Generic contract binding to set the session for
	CallOpts     bind.CallOpts             // Call options to use throughout this session
	TransactOpts bind.TransactOpts         // Transaction auth options to use throughout this session
}

// BatchCrossChainMessengerCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type BatchCrossChainMessengerCallerSession struct {
	Contract *BatchCrossChainMessengerCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts                   // Call options to use throughout this session
}

// BatchCrossChainMessengerTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type BatchCrossChainMessengerTransactorSession struct {
	Contract     *BatchCrossChainMessengerTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts                   // Transaction auth options to use throughout this session
}

// BatchCrossChainMessengerRaw is an auto generated low-level Go binding around an Ethereum contract.
type BatchCrossChainMessengerRaw struct {
	Contract *BatchCrossChainMessenger // Generic contract binding to access the raw methods on
}

// BatchCrossChainMessengerCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type BatchCrossChainMessengerCallerRaw struct {
	Contract *BatchCrossChainMessengerCaller // Generic read-only contract binding to access the raw methods on
}

// BatchCrossChainMessengerTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type BatchCrossChainMessengerTransactorRaw struct {
	Contract *BatchCrossChainMessengerTransactor // Generic write-only contract binding to access the raw methods on
}

// NewBatchCrossChainMessenger creates a new instance of BatchCrossChainMessenger, bound to a specific deployed contract.
func NewBatchCrossChainMessenger(address common.Address, backend bind.ContractBackend) (*BatchCrossChainMessenger, error) {
	contract, err := bindBatchCrossChainMessenger(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &BatchCrossChainMessenger{BatchCrossChainMessengerCaller: BatchCrossChainMessengerCaller{contract: contract}, BatchCrossChainMessengerTransactor: BatchCrossChainMessengerTransactor{contract: contract}, BatchCrossChainMessengerFilterer: BatchCrossChainMessengerFilterer{contract: contract}}, nil
}

// NewBatchCrossChainMessengerCaller creates a new read-only instance of BatchCrossChainMessenger, bound to a specific deployed contract.
func NewBatchCrossChainMessengerCaller(address common.Address, caller bind.ContractCaller) (*BatchCrossChainMessengerCaller, error) {
	contract, err := bindBatchCrossChainMessenger(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &BatchCrossChainMessengerCaller{contract: contract}, nil
}

// NewBatchCrossChainMessengerTransactor creates a new write-only instance of BatchCrossChainMessenger, bound to a specific deployed contract.
func NewBatchCrossChainMessengerTransactor(address common.Address, transactor bind.ContractTransactor) (*BatchCrossChainMessengerTransactor, error) {
	contract, err := bindBatchCrossChainMessenger(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &BatchCrossChainMessengerTransactor{contract: contract}, nil
}

// NewBatchCrossChainMessengerFilterer creates a new log filterer instance of BatchCrossChainMessenger, bound to a specific deployed contract.
func NewBatchCrossChainMessengerFilterer(address common.Address, filterer bind.ContractFilterer) (*BatchCrossChainMessengerFilterer, error) {
	contract, err := bindBatchCrossChainMessenger(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &BatchCrossChainMessengerFilterer{contract: contract}, nil
}

// bindBatchCrossChainMessenger binds a generic wrapper to an already deployed contract.
func bindBatchCrossChainMessenger(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := BatchCrossChainMessengerMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_BatchCrossChainMessenger *BatchCrossChainMessengerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _BatchCrossChainMessenger.Contract.BatchCrossChainMessengerCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_BatchCrossChainMessenger *BatchCrossChainMessengerRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.Contract.BatchCrossChainMessengerTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_BatchCrossChainMessenger *BatchCrossChainMessengerRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.Contract.BatchCrossChainMessengerTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_BatchCrossChainMessenger *BatchCrossChainMessengerCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _BatchCrossChainMessenger.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_BatchCrossChainMessenger *BatchCrossChainMessengerTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_BatchCrossChainMessenger *BatchCrossChainMessengerTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.Contract.contract.Transact(opts, method, params...)
}

// GetCurrentMessages is a free data retrieval call binding the contract method 0xc1329fcb.
//
// Solidity: function getCurrentMessages(bytes32 sourceBlockchainID) view returns(address, string[])
func (_BatchCrossChainMessenger *BatchCrossChainMessengerCaller) GetCurrentMessages(opts *bind.CallOpts, sourceBlockchainID [32]byte) (common.Address, []string, error) {
	var out []interface{}
	err := _BatchCrossChainMessenger.contract.Call(opts, &out, "getCurrentMessages", sourceBlockchainID)

	if err != nil {
		return *new(common.Address), *new([]string), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	out1 := *abi.ConvertType(out[1], new([]string)).(*[]string)

	return out0, out1, err

}

// GetCurrentMessages is a free data retrieval call binding the contract method 0xc1329fcb.
//
// Solidity: function getCurrentMessages(bytes32 sourceBlockchainID) view returns(address, string[])
func (_BatchCrossChainMessenger *BatchCrossChainMessengerSession) GetCurrentMessages(sourceBlockchainID [32]byte) (common.Address, []string, error) {
	return _BatchCrossChainMessenger.Contract.GetCurrentMessages(&_BatchCrossChainMessenger.CallOpts, sourceBlockchainID)
}

// GetCurrentMessages is a free data retrieval call binding the contract method 0xc1329fcb.
//
// Solidity: function getCurrentMessages(bytes32 sourceBlockchainID) view returns(address, string[])
func (_BatchCrossChainMessenger *BatchCrossChainMessengerCallerSession) GetCurrentMessages(sourceBlockchainID [32]byte) (common.Address, []string, error) {
	return _BatchCrossChainMessenger.Contract.GetCurrentMessages(&_BatchCrossChainMessenger.CallOpts, sourceBlockchainID)
}

// GetMinTeleporterVersion is a free data retrieval call binding the contract method 0xd2cc7a70.
//
// Solidity: function getMinTeleporterVersion() view returns(uint256)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerCaller) GetMinTeleporterVersion(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _BatchCrossChainMessenger.contract.Call(opts, &out, "getMinTeleporterVersion")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetMinTeleporterVersion is a free data retrieval call binding the contract method 0xd2cc7a70.
//
// Solidity: function getMinTeleporterVersion() view returns(uint256)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerSession) GetMinTeleporterVersion() (*big.Int, error) {
	return _BatchCrossChainMessenger.Contract.GetMinTeleporterVersion(&_BatchCrossChainMessenger.CallOpts)
}

// GetMinTeleporterVersion is a free data retrieval call binding the contract method 0xd2cc7a70.
//
// Solidity: function getMinTeleporterVersion() view returns(uint256)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerCallerSession) GetMinTeleporterVersion() (*big.Int, error) {
	return _BatchCrossChainMessenger.Contract.GetMinTeleporterVersion(&_BatchCrossChainMessenger.CallOpts)
}

// IsTeleporterAddressPaused is a free data retrieval call binding the contract method 0x97314297.
//
// Solidity: function isTeleporterAddressPaused(address teleporterAddress) view returns(bool)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerCaller) IsTeleporterAddressPaused(opts *bind.CallOpts, teleporterAddress common.Address) (bool, error) {
	var out []interface{}
	err := _BatchCrossChainMessenger.contract.Call(opts, &out, "isTeleporterAddressPaused", teleporterAddress)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsTeleporterAddressPaused is a free data retrieval call binding the contract method 0x97314297.
//
// Solidity: function isTeleporterAddressPaused(address teleporterAddress) view returns(bool)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerSession) IsTeleporterAddressPaused(teleporterAddress common.Address) (bool, error) {
	return _BatchCrossChainMessenger.Contract.IsTeleporterAddressPaused(&_BatchCrossChainMessenger.CallOpts, teleporterAddress)
}

// IsTeleporterAddressPaused is a free data retrieval call binding the contract method 0x97314297.
//
// Solidity: function isTeleporterAddressPaused(address teleporterAddress) view returns(bool)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerCallerSession) IsTeleporterAddressPaused(teleporterAddress common.Address) (bool, error) {
	return _BatchCrossChainMessenger.Contract.IsTeleporterAddressPaused(&_BatchCrossChainMessenger.CallOpts, teleporterAddress)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _BatchCrossChainMessenger.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerSession) Owner() (common.Address, error) {
	return _BatchCrossChainMessenger.Contract.Owner(&_BatchCrossChainMessenger.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerCallerSession) Owner() (common.Address, error) {
	return _BatchCrossChainMessenger.Contract.Owner(&_BatchCrossChainMessenger.CallOpts)
}

// TeleporterRegistry is a free data retrieval call binding the contract method 0x1a7f5bec.
//
// Solidity: function teleporterRegistry() view returns(address)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerCaller) TeleporterRegistry(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _BatchCrossChainMessenger.contract.Call(opts, &out, "teleporterRegistry")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// TeleporterRegistry is a free data retrieval call binding the contract method 0x1a7f5bec.
//
// Solidity: function teleporterRegistry() view returns(address)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerSession) TeleporterRegistry() (common.Address, error) {
	return _BatchCrossChainMessenger.Contract.TeleporterRegistry(&_BatchCrossChainMessenger.CallOpts)
}

// TeleporterRegistry is a free data retrieval call binding the contract method 0x1a7f5bec.
//
// Solidity: function teleporterRegistry() view returns(address)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerCallerSession) TeleporterRegistry() (common.Address, error) {
	return _BatchCrossChainMessenger.Contract.TeleporterRegistry(&_BatchCrossChainMessenger.CallOpts)
}

// PauseTeleporterAddress is a paid mutator transaction binding the contract method 0x2b0d8f18.
//
// Solidity: function pauseTeleporterAddress(address teleporterAddress) returns()
func (_BatchCrossChainMessenger *BatchCrossChainMessengerTransactor) PauseTeleporterAddress(opts *bind.TransactOpts, teleporterAddress common.Address) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.contract.Transact(opts, "pauseTeleporterAddress", teleporterAddress)
}

// PauseTeleporterAddress is a paid mutator transaction binding the contract method 0x2b0d8f18.
//
// Solidity: function pauseTeleporterAddress(address teleporterAddress) returns()
func (_BatchCrossChainMessenger *BatchCrossChainMessengerSession) PauseTeleporterAddress(teleporterAddress common.Address) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.Contract.PauseTeleporterAddress(&_BatchCrossChainMessenger.TransactOpts, teleporterAddress)
}

// PauseTeleporterAddress is a paid mutator transaction binding the contract method 0x2b0d8f18.
//
// Solidity: function pauseTeleporterAddress(address teleporterAddress) returns()
func (_BatchCrossChainMessenger *BatchCrossChainMessengerTransactorSession) PauseTeleporterAddress(teleporterAddress common.Address) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.Contract.PauseTeleporterAddress(&_BatchCrossChainMessenger.TransactOpts, teleporterAddress)
}

// ReceiveTeleporterMessage is a paid mutator transaction binding the contract method 0xc868efaa.
//
// Solidity: function receiveTeleporterMessage(bytes32 sourceBlockchainID, address originSenderAddress, bytes message) returns()
func (_BatchCrossChainMessenger *BatchCrossChainMessengerTransactor) ReceiveTeleporterMessage(opts *bind.TransactOpts, sourceBlockchainID [32]byte, originSenderAddress common.Address, message []byte) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.contract.Transact(opts, "receiveTeleporterMessage", sourceBlockchainID, originSenderAddress, message)
}

// ReceiveTeleporterMessage is a paid mutator transaction binding the contract method 0xc868efaa.
//
// Solidity: function receiveTeleporterMessage(bytes32 sourceBlockchainID, address originSenderAddress, bytes message) returns()
func (_BatchCrossChainMessenger *BatchCrossChainMessengerSession) ReceiveTeleporterMessage(sourceBlockchainID [32]byte, originSenderAddress common.Address, message []byte) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.Contract.ReceiveTeleporterMessage(&_BatchCrossChainMessenger.TransactOpts, sourceBlockchainID, originSenderAddress, message)
}

// ReceiveTeleporterMessage is a paid mutator transaction binding the contract method 0xc868efaa.
//
// Solidity: function receiveTeleporterMessage(bytes32 sourceBlockchainID, address originSenderAddress, bytes message) returns()
func (_BatchCrossChainMessenger *BatchCrossChainMessengerTransactorSession) ReceiveTeleporterMessage(sourceBlockchainID [32]byte, originSenderAddress common.Address, message []byte) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.Contract.ReceiveTeleporterMessage(&_BatchCrossChainMessenger.TransactOpts, sourceBlockchainID, originSenderAddress, message)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_BatchCrossChainMessenger *BatchCrossChainMessengerTransactor) RenounceOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.contract.Transact(opts, "renounceOwnership")
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_BatchCrossChainMessenger *BatchCrossChainMessengerSession) RenounceOwnership() (*types.Transaction, error) {
	return _BatchCrossChainMessenger.Contract.RenounceOwnership(&_BatchCrossChainMessenger.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_BatchCrossChainMessenger *BatchCrossChainMessengerTransactorSession) RenounceOwnership() (*types.Transaction, error) {
	return _BatchCrossChainMessenger.Contract.RenounceOwnership(&_BatchCrossChainMessenger.TransactOpts)
}

// SendMessages is a paid mutator transaction binding the contract method 0x3902970c.
//
// Solidity: function sendMessages(bytes32 destinationBlockchainID, address destinationAddress, address feeTokenAddress, uint256 feeAmount, uint256 requiredGasLimit, string[] messages) returns(bytes32[])
func (_BatchCrossChainMessenger *BatchCrossChainMessengerTransactor) SendMessages(opts *bind.TransactOpts, destinationBlockchainID [32]byte, destinationAddress common.Address, feeTokenAddress common.Address, feeAmount *big.Int, requiredGasLimit *big.Int, messages []string) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.contract.Transact(opts, "sendMessages", destinationBlockchainID, destinationAddress, feeTokenAddress, feeAmount, requiredGasLimit, messages)
}

// SendMessages is a paid mutator transaction binding the contract method 0x3902970c.
//
// Solidity: function sendMessages(bytes32 destinationBlockchainID, address destinationAddress, address feeTokenAddress, uint256 feeAmount, uint256 requiredGasLimit, string[] messages) returns(bytes32[])
func (_BatchCrossChainMessenger *BatchCrossChainMessengerSession) SendMessages(destinationBlockchainID [32]byte, destinationAddress common.Address, feeTokenAddress common.Address, feeAmount *big.Int, requiredGasLimit *big.Int, messages []string) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.Contract.SendMessages(&_BatchCrossChainMessenger.TransactOpts, destinationBlockchainID, destinationAddress, feeTokenAddress, feeAmount, requiredGasLimit, messages)
}

// SendMessages is a paid mutator transaction binding the contract method 0x3902970c.
//
// Solidity: function sendMessages(bytes32 destinationBlockchainID, address destinationAddress, address feeTokenAddress, uint256 feeAmount, uint256 requiredGasLimit, string[] messages) returns(bytes32[])
func (_BatchCrossChainMessenger *BatchCrossChainMessengerTransactorSession) SendMessages(destinationBlockchainID [32]byte, destinationAddress common.Address, feeTokenAddress common.Address, feeAmount *big.Int, requiredGasLimit *big.Int, messages []string) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.Contract.SendMessages(&_BatchCrossChainMessenger.TransactOpts, destinationBlockchainID, destinationAddress, feeTokenAddress, feeAmount, requiredGasLimit, messages)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_BatchCrossChainMessenger *BatchCrossChainMessengerTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_BatchCrossChainMessenger *BatchCrossChainMessengerSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.Contract.TransferOwnership(&_BatchCrossChainMessenger.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_BatchCrossChainMessenger *BatchCrossChainMessengerTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.Contract.TransferOwnership(&_BatchCrossChainMessenger.TransactOpts, newOwner)
}

// UnpauseTeleporterAddress is a paid mutator transaction binding the contract method 0x4511243e.
//
// Solidity: function unpauseTeleporterAddress(address teleporterAddress) returns()
func (_BatchCrossChainMessenger *BatchCrossChainMessengerTransactor) UnpauseTeleporterAddress(opts *bind.TransactOpts, teleporterAddress common.Address) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.contract.Transact(opts, "unpauseTeleporterAddress", teleporterAddress)
}

// UnpauseTeleporterAddress is a paid mutator transaction binding the contract method 0x4511243e.
//
// Solidity: function unpauseTeleporterAddress(address teleporterAddress) returns()
func (_BatchCrossChainMessenger *BatchCrossChainMessengerSession) UnpauseTeleporterAddress(teleporterAddress common.Address) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.Contract.UnpauseTeleporterAddress(&_BatchCrossChainMessenger.TransactOpts, teleporterAddress)
}

// UnpauseTeleporterAddress is a paid mutator transaction binding the contract method 0x4511243e.
//
// Solidity: function unpauseTeleporterAddress(address teleporterAddress) returns()
func (_BatchCrossChainMessenger *BatchCrossChainMessengerTransactorSession) UnpauseTeleporterAddress(teleporterAddress common.Address) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.Contract.UnpauseTeleporterAddress(&_BatchCrossChainMessenger.TransactOpts, teleporterAddress)
}

// UpdateMinTeleporterVersion is a paid mutator transaction binding the contract method 0x5eb99514.
//
// Solidity: function updateMinTeleporterVersion(uint256 version) returns()
func (_BatchCrossChainMessenger *BatchCrossChainMessengerTransactor) UpdateMinTeleporterVersion(opts *bind.TransactOpts, version *big.Int) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.contract.Transact(opts, "updateMinTeleporterVersion", version)
}

// UpdateMinTeleporterVersion is a paid mutator transaction binding the contract method 0x5eb99514.
//
// Solidity: function updateMinTeleporterVersion(uint256 version) returns()
func (_BatchCrossChainMessenger *BatchCrossChainMessengerSession) UpdateMinTeleporterVersion(version *big.Int) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.Contract.UpdateMinTeleporterVersion(&_BatchCrossChainMessenger.TransactOpts, version)
}

// UpdateMinTeleporterVersion is a paid mutator transaction binding the contract method 0x5eb99514.
//
// Solidity: function updateMinTeleporterVersion(uint256 version) returns()
func (_BatchCrossChainMessenger *BatchCrossChainMessengerTransactorSession) UpdateMinTeleporterVersion(version *big.Int) (*types.Transaction, error) {
	return _BatchCrossChainMessenger.Contract.UpdateMinTeleporterVersion(&_BatchCrossChainMessenger.TransactOpts, version)
}

// BatchCrossChainMessengerMinTeleporterVersionUpdatedIterator is returned from FilterMinTeleporterVersionUpdated and is used to iterate over the raw logs and unpacked data for MinTeleporterVersionUpdated events raised by the BatchCrossChainMessenger contract.
type BatchCrossChainMessengerMinTeleporterVersionUpdatedIterator struct {
	Event *BatchCrossChainMessengerMinTeleporterVersionUpdated // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log          // Log channel receiving the found contract events
	sub  interfaces.Subscription // Subscription for errors, completion and termination
	done bool                    // Whether the subscription completed delivering logs
	fail error                   // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *BatchCrossChainMessengerMinTeleporterVersionUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(BatchCrossChainMessengerMinTeleporterVersionUpdated)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(BatchCrossChainMessengerMinTeleporterVersionUpdated)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *BatchCrossChainMessengerMinTeleporterVersionUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *BatchCrossChainMessengerMinTeleporterVersionUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// BatchCrossChainMessengerMinTeleporterVersionUpdated represents a MinTeleporterVersionUpdated event raised by the BatchCrossChainMessenger contract.
type BatchCrossChainMessengerMinTeleporterVersionUpdated struct {
	OldMinTeleporterVersion *big.Int
	NewMinTeleporterVersion *big.Int
	Raw                     types.Log // Blockchain specific contextual infos
}

// FilterMinTeleporterVersionUpdated is a free log retrieval operation binding the contract event 0xa9a7ef57e41f05b4c15480842f5f0c27edfcbb553fed281f7c4068452cc1c02d.
//
// Solidity: event MinTeleporterVersionUpdated(uint256 indexed oldMinTeleporterVersion, uint256 indexed newMinTeleporterVersion)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerFilterer) FilterMinTeleporterVersionUpdated(opts *bind.FilterOpts, oldMinTeleporterVersion []*big.Int, newMinTeleporterVersion []*big.Int) (*BatchCrossChainMessengerMinTeleporterVersionUpdatedIterator, error) {

	var oldMinTeleporterVersionRule []interface{}
	for _, oldMinTeleporterVersionItem := range oldMinTeleporterVersion {
		oldMinTeleporterVersionRule = append(oldMinTeleporterVersionRule, oldMinTeleporterVersionItem)
	}
	var newMinTeleporterVersionRule []interface{}
	for _, newMinTeleporterVersionItem := range newMinTeleporterVersion {
		newMinTeleporterVersionRule = append(newMinTeleporterVersionRule, newMinTeleporterVersionItem)
	}

	logs, sub, err := _BatchCrossChainMessenger.contract.FilterLogs(opts, "MinTeleporterVersionUpdated", oldMinTeleporterVersionRule, newMinTeleporterVersionRule)
	if err != nil {
		return nil, err
	}
	return &BatchCrossChainMessengerMinTeleporterVersionUpdatedIterator{contract: _BatchCrossChainMessenger.contract, event: "MinTeleporterVersionUpdated", logs: logs, sub: sub}, nil
}

// WatchMinTeleporterVersionUpdated is a free log subscription operation binding the contract event 0xa9a7ef57e41f05b4c15480842f5f0c27edfcbb553fed281f7c4068452cc1c02d.
//
// Solidity: event MinTeleporterVersionUpdated(uint256 indexed oldMinTeleporterVersion, uint256 indexed newMinTeleporterVersion)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerFilterer) WatchMinTeleporterVersionUpdated(opts *bind.WatchOpts, sink chan<- *BatchCrossChainMessengerMinTeleporterVersionUpdated, oldMinTeleporterVersion []*big.Int, newMinTeleporterVersion []*big.Int) (event.Subscription, error) {

	var oldMinTeleporterVersionRule []interface{}
	for _, oldMinTeleporterVersionItem := range oldMinTeleporterVersion {
		oldMinTeleporterVersionRule = append(oldMinTeleporterVersionRule, oldMinTeleporterVersionItem)
	}
	var newMinTeleporterVersionRule []interface{}
	for _, newMinTeleporterVersionItem := range newMinTeleporterVersion {
		newMinTeleporterVersionRule = append(newMinTeleporterVersionRule, newMinTeleporterVersionItem)
	}

	logs, sub, err := _BatchCrossChainMessenger.contract.WatchLogs(opts, "MinTeleporterVersionUpdated", oldMinTeleporterVersionRule, newMinTeleporterVersionRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(BatchCrossChainMessengerMinTeleporterVersionUpdated)
				if err := _BatchCrossChainMessenger.contract.UnpackLog(event, "MinTeleporterVersionUpdated", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseMinTeleporterVersionUpdated is a log parse operation binding the contract event 0xa9a7ef57e41f05b4c15480842f5f0c27edfcbb553fed281f7c4068452cc1c02d.
//
// Solidity: event MinTeleporterVersionUpdated(uint256 indexed oldMinTeleporterVersion, uint256 indexed newMinTeleporterVersion)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerFilterer) ParseMinTeleporterVersionUpdated(log types.Log) (*BatchCrossChainMessengerMinTeleporterVersionUpdated, error) {
	event := new(BatchCrossChainMessengerMinTeleporterVersionUpdated)
	if err := _BatchCrossChainMessenger.contract.UnpackLog(event, "MinTeleporterVersionUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// BatchCrossChainMessengerOwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the BatchCrossChainMessenger contract.
type BatchCrossChainMessengerOwnershipTransferredIterator struct {
	Event *BatchCrossChainMessengerOwnershipTransferred // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log          // Log channel receiving the found contract events
	sub  interfaces.Subscription // Subscription for errors, completion and termination
	done bool                    // Whether the subscription completed delivering logs
	fail error                   // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *BatchCrossChainMessengerOwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(BatchCrossChainMessengerOwnershipTransferred)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(BatchCrossChainMessengerOwnershipTransferred)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *BatchCrossChainMessengerOwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *BatchCrossChainMessengerOwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// BatchCrossChainMessengerOwnershipTransferred represents a OwnershipTransferred event raised by the BatchCrossChainMessenger contract.
type BatchCrossChainMessengerOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerFilterer) FilterOwnershipTransferred(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*BatchCrossChainMessengerOwnershipTransferredIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _BatchCrossChainMessenger.contract.FilterLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &BatchCrossChainMessengerOwnershipTransferredIterator{contract: _BatchCrossChainMessenger.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerFilterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *BatchCrossChainMessengerOwnershipTransferred, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _BatchCrossChainMessenger.contract.WatchLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(BatchCrossChainMessengerOwnershipTransferred)
				if err := _BatchCrossChainMessenger.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseOwnershipTransferred is a log parse operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerFilterer) ParseOwnershipTransferred(log types.Log) (*BatchCrossChainMessengerOwnershipTransferred, error) {
	event := new(BatchCrossChainMessengerOwnershipTransferred)
	if err := _BatchCrossChainMessenger.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// BatchCrossChainMessengerReceiveMessagesIterator is returned from FilterReceiveMessages and is used to iterate over the raw logs and unpacked data for ReceiveMessages events raised by the BatchCrossChainMessenger contract.
type BatchCrossChainMessengerReceiveMessagesIterator struct {
	Event *BatchCrossChainMessengerReceiveMessages // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log          // Log channel receiving the found contract events
	sub  interfaces.Subscription // Subscription for errors, completion and termination
	done bool                    // Whether the subscription completed delivering logs
	fail error                   // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *BatchCrossChainMessengerReceiveMessagesIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(BatchCrossChainMessengerReceiveMessages)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(BatchCrossChainMessengerReceiveMessages)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *BatchCrossChainMessengerReceiveMessagesIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *BatchCrossChainMessengerReceiveMessagesIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// BatchCrossChainMessengerReceiveMessages represents a ReceiveMessages event raised by the BatchCrossChainMessenger contract.
type BatchCrossChainMessengerReceiveMessages struct {
	SourceBlockchainID  [32]byte
	OriginSenderAddress common.Address
	Messages            []string
	Raw                 types.Log // Blockchain specific contextual infos
}

// FilterReceiveMessages is a free log retrieval operation binding the contract event 0x903a123a8d947dcbd005d05b20bb5180f878c6d3527681c1abb1d8987714cb9d.
//
// Solidity: event ReceiveMessages(bytes32 indexed sourceBlockchainID, address indexed originSenderAddress, string[] messages)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerFilterer) FilterReceiveMessages(opts *bind.FilterOpts, sourceBlockchainID [][32]byte, originSenderAddress []common.Address) (*BatchCrossChainMessengerReceiveMessagesIterator, error) {

	var sourceBlockchainIDRule []interface{}
	for _, sourceBlockchainIDItem := range sourceBlockchainID {
		sourceBlockchainIDRule = append(sourceBlockchainIDRule, sourceBlockchainIDItem)
	}
	var originSenderAddressRule []interface{}
	for _, originSenderAddressItem := range originSenderAddress {
		originSenderAddressRule = append(originSenderAddressRule, originSenderAddressItem)
	}

	logs, sub, err := _BatchCrossChainMessenger.contract.FilterLogs(opts, "ReceiveMessages", sourceBlockchainIDRule, originSenderAddressRule)
	if err != nil {
		return nil, err
	}
	return &BatchCrossChainMessengerReceiveMessagesIterator{contract: _BatchCrossChainMessenger.contract, event: "ReceiveMessages", logs: logs, sub: sub}, nil
}

// WatchReceiveMessages is a free log subscription operation binding the contract event 0x903a123a8d947dcbd005d05b20bb5180f878c6d3527681c1abb1d8987714cb9d.
//
// Solidity: event ReceiveMessages(bytes32 indexed sourceBlockchainID, address indexed originSenderAddress, string[] messages)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerFilterer) WatchReceiveMessages(opts *bind.WatchOpts, sink chan<- *BatchCrossChainMessengerReceiveMessages, sourceBlockchainID [][32]byte, originSenderAddress []common.Address) (event.Subscription, error) {

	var sourceBlockchainIDRule []interface{}
	for _, sourceBlockchainIDItem := range sourceBlockchainID {
		sourceBlockchainIDRule = append(sourceBlockchainIDRule, sourceBlockchainIDItem)
	}
	var originSenderAddressRule []interface{}
	for _, originSenderAddressItem := range originSenderAddress {
		originSenderAddressRule = append(originSenderAddressRule, originSenderAddressItem)
	}

	logs, sub, err := _BatchCrossChainMessenger.contract.WatchLogs(opts, "ReceiveMessages", sourceBlockchainIDRule, originSenderAddressRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(BatchCrossChainMessengerReceiveMessages)
				if err := _BatchCrossChainMessenger.contract.UnpackLog(event, "ReceiveMessages", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseReceiveMessages is a log parse operation binding the contract event 0x903a123a8d947dcbd005d05b20bb5180f878c6d3527681c1abb1d8987714cb9d.
//
// Solidity: event ReceiveMessages(bytes32 indexed sourceBlockchainID, address indexed originSenderAddress, string[] messages)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerFilterer) ParseReceiveMessages(log types.Log) (*BatchCrossChainMessengerReceiveMessages, error) {
	event := new(BatchCrossChainMessengerReceiveMessages)
	if err := _BatchCrossChainMessenger.contract.UnpackLog(event, "ReceiveMessages", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// BatchCrossChainMessengerSendMessagesIterator is returned from FilterSendMessages and is used to iterate over the raw logs and unpacked data for SendMessages events raised by the BatchCrossChainMessenger contract.
type BatchCrossChainMessengerSendMessagesIterator struct {
	Event *BatchCrossChainMessengerSendMessages // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log          // Log channel receiving the found contract events
	sub  interfaces.Subscription // Subscription for errors, completion and termination
	done bool                    // Whether the subscription completed delivering logs
	fail error                   // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *BatchCrossChainMessengerSendMessagesIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(BatchCrossChainMessengerSendMessages)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(BatchCrossChainMessengerSendMessages)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *BatchCrossChainMessengerSendMessagesIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *BatchCrossChainMessengerSendMessagesIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// BatchCrossChainMessengerSendMessages represents a SendMessages event raised by the BatchCrossChainMessenger contract.
type BatchCrossChainMessengerSendMessages struct {
	DestinationBlockchainID [32]byte
	DestinationAddress      common.Address
	FeeTokenAddress         common.Address
	FeeAmount               *big.Int
	RequiredGasLimit        *big.Int
	Messages                []string
	Raw                     types.Log // Blockchain specific contextual infos
}

// FilterSendMessages is a free log retrieval operation binding the contract event 0x430d1906813fdb2129a19139f4112a1396804605501a798df3a4042590ba20d5.
//
// Solidity: event SendMessages(bytes32 indexed destinationBlockchainID, address indexed destinationAddress, address feeTokenAddress, uint256 feeAmount, uint256 requiredGasLimit, string[] messages)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerFilterer) FilterSendMessages(opts *bind.FilterOpts, destinationBlockchainID [][32]byte, destinationAddress []common.Address) (*BatchCrossChainMessengerSendMessagesIterator, error) {

	var destinationBlockchainIDRule []interface{}
	for _, destinationBlockchainIDItem := range destinationBlockchainID {
		destinationBlockchainIDRule = append(destinationBlockchainIDRule, destinationBlockchainIDItem)
	}
	var destinationAddressRule []interface{}
	for _, destinationAddressItem := range destinationAddress {
		destinationAddressRule = append(destinationAddressRule, destinationAddressItem)
	}

	logs, sub, err := _BatchCrossChainMessenger.contract.FilterLogs(opts, "SendMessages", destinationBlockchainIDRule, destinationAddressRule)
	if err != nil {
		return nil, err
	}
	return &BatchCrossChainMessengerSendMessagesIterator{contract: _BatchCrossChainMessenger.contract, event: "SendMessages", logs: logs, sub: sub}, nil
}

// WatchSendMessages is a free log subscription operation binding the contract event 0x430d1906813fdb2129a19139f4112a1396804605501a798df3a4042590ba20d5.
//
// Solidity: event SendMessages(bytes32 indexed destinationBlockchainID, address indexed destinationAddress, address feeTokenAddress, uint256 feeAmount, uint256 requiredGasLimit, string[] messages)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerFilterer) WatchSendMessages(opts *bind.WatchOpts, sink chan<- *BatchCrossChainMessengerSendMessages, destinationBlockchainID [][32]byte, destinationAddress []common.Address) (event.Subscription, error) {

	var destinationBlockchainIDRule []interface{}
	for _, destinationBlockchainIDItem := range destinationBlockchainID {
		destinationBlockchainIDRule = append(destinationBlockchainIDRule, destinationBlockchainIDItem)
	}
	var destinationAddressRule []interface{}
	for _, destinationAddressItem := range destinationAddress {
		destinationAddressRule = append(destinationAddressRule, destinationAddressItem)
	}

	logs, sub, err := _BatchCrossChainMessenger.contract.WatchLogs(opts, "SendMessages", destinationBlockchainIDRule, destinationAddressRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(BatchCrossChainMessengerSendMessages)
				if err := _BatchCrossChainMessenger.contract.UnpackLog(event, "SendMessages", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseSendMessages is a log parse operation binding the contract event 0x430d1906813fdb2129a19139f4112a1396804605501a798df3a4042590ba20d5.
//
// Solidity: event SendMessages(bytes32 indexed destinationBlockchainID, address indexed destinationAddress, address feeTokenAddress, uint256 feeAmount, uint256 requiredGasLimit, string[] messages)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerFilterer) ParseSendMessages(log types.Log) (*BatchCrossChainMessengerSendMessages, error) {
	event := new(BatchCrossChainMessengerSendMessages)
	if err := _BatchCrossChainMessenger.contract.UnpackLog(event, "SendMessages", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// BatchCrossChainMessengerTeleporterAddressPausedIterator is returned from FilterTeleporterAddressPaused and is used to iterate over the raw logs and unpacked data for TeleporterAddressPaused events raised by the BatchCrossChainMessenger contract.
type BatchCrossChainMessengerTeleporterAddressPausedIterator struct {
	Event *BatchCrossChainMessengerTeleporterAddressPaused // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log          // Log channel receiving the found contract events
	sub  interfaces.Subscription // Subscription for errors, completion and termination
	done bool                    // Whether the subscription completed delivering logs
	fail error                   // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *BatchCrossChainMessengerTeleporterAddressPausedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(BatchCrossChainMessengerTeleporterAddressPaused)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(BatchCrossChainMessengerTeleporterAddressPaused)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *BatchCrossChainMessengerTeleporterAddressPausedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *BatchCrossChainMessengerTeleporterAddressPausedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// BatchCrossChainMessengerTeleporterAddressPaused represents a TeleporterAddressPaused event raised by the BatchCrossChainMessenger contract.
type BatchCrossChainMessengerTeleporterAddressPaused struct {
	TeleporterAddress common.Address
	Raw               types.Log // Blockchain specific contextual infos
}

// FilterTeleporterAddressPaused is a free log retrieval operation binding the contract event 0x933f93e57a222e6330362af8b376d0a8725b6901e9a2fb86d00f169702b28a4c.
//
// Solidity: event TeleporterAddressPaused(address indexed teleporterAddress)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerFilterer) FilterTeleporterAddressPaused(opts *bind.FilterOpts, teleporterAddress []common.Address) (*BatchCrossChainMessengerTeleporterAddressPausedIterator, error) {

	var teleporterAddressRule []interface{}
	for _, teleporterAddressItem := range teleporterAddress {
		teleporterAddressRule = append(teleporterAddressRule, teleporterAddressItem)
	}

	logs, sub, err := _BatchCrossChainMessenger.contract.FilterLogs(opts, "TeleporterAddressPaused", teleporterAddressRule)
	if err != nil {
		return nil, err
	}
	return &BatchCrossChainMessengerTeleporterAddressPausedIterator{contract: _BatchCrossChainMessenger.contract, event: "TeleporterAddressPaused", logs: logs, sub: sub}, nil
}

// WatchTeleporterAddressPaused is a free log subscription operation binding the contract event 0x933f93e57a222e6330362af8b376d0a8725b6901e9a2fb86d00f169702b28a4c.
//
// Solidity: event TeleporterAddressPaused(address indexed teleporterAddress)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerFilterer) WatchTeleporterAddressPaused(opts *bind.WatchOpts, sink chan<- *BatchCrossChainMessengerTeleporterAddressPaused, teleporterAddress []common.Address) (event.Subscription, error) {

	var teleporterAddressRule []interface{}
	for _, teleporterAddressItem := range teleporterAddress {
		teleporterAddressRule = append(teleporterAddressRule, teleporterAddressItem)
	}

	logs, sub, err := _BatchCrossChainMessenger.contract.WatchLogs(opts, "TeleporterAddressPaused", teleporterAddressRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(BatchCrossChainMessengerTeleporterAddressPaused)
				if err := _BatchCrossChainMessenger.contract.UnpackLog(event, "TeleporterAddressPaused", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTeleporterAddressPaused is a log parse operation binding the contract event 0x933f93e57a222e6330362af8b376d0a8725b6901e9a2fb86d00f169702b28a4c.
//
// Solidity: event TeleporterAddressPaused(address indexed teleporterAddress)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerFilterer) ParseTeleporterAddressPaused(log types.Log) (*BatchCrossChainMessengerTeleporterAddressPaused, error) {
	event := new(BatchCrossChainMessengerTeleporterAddressPaused)
	if err := _BatchCrossChainMessenger.contract.UnpackLog(event, "TeleporterAddressPaused", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// BatchCrossChainMessengerTeleporterAddressUnpausedIterator is returned from FilterTeleporterAddressUnpaused and is used to iterate over the raw logs and unpacked data for TeleporterAddressUnpaused events raised by the BatchCrossChainMessenger contract.
type BatchCrossChainMessengerTeleporterAddressUnpausedIterator struct {
	Event *BatchCrossChainMessengerTeleporterAddressUnpaused // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log          // Log channel receiving the found contract events
	sub  interfaces.Subscription // Subscription for errors, completion and termination
	done bool                    // Whether the subscription completed delivering logs
	fail error                   // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *BatchCrossChainMessengerTeleporterAddressUnpausedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(BatchCrossChainMessengerTeleporterAddressUnpaused)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(BatchCrossChainMessengerTeleporterAddressUnpaused)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *BatchCrossChainMessengerTeleporterAddressUnpausedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *BatchCrossChainMessengerTeleporterAddressUnpausedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// BatchCrossChainMessengerTeleporterAddressUnpaused represents a TeleporterAddressUnpaused event raised by the BatchCrossChainMessenger contract.
type BatchCrossChainMessengerTeleporterAddressUnpaused struct {
	TeleporterAddress common.Address
	Raw               types.Log // Blockchain specific contextual infos
}

// FilterTeleporterAddressUnpaused is a free log retrieval operation binding the contract event 0x844e2f3154214672229235858fd029d1dfd543901c6d05931f0bc2480a2d72c3.
//
// Solidity: event TeleporterAddressUnpaused(address indexed teleporterAddress)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerFilterer) FilterTeleporterAddressUnpaused(opts *bind.FilterOpts, teleporterAddress []common.Address) (*BatchCrossChainMessengerTeleporterAddressUnpausedIterator, error) {

	var teleporterAddressRule []interface{}
	for _, teleporterAddressItem := range teleporterAddress {
		teleporterAddressRule = append(teleporterAddressRule, teleporterAddressItem)
	}

	logs, sub, err := _BatchCrossChainMessenger.contract.FilterLogs(opts, "TeleporterAddressUnpaused", teleporterAddressRule)
	if err != nil {
		return nil, err
	}
	return &BatchCrossChainMessengerTeleporterAddressUnpausedIterator{contract: _BatchCrossChainMessenger.contract, event: "TeleporterAddressUnpaused", logs: logs, sub: sub}, nil
}

// WatchTeleporterAddressUnpaused is a free log subscription operation binding the contract event 0x844e2f3154214672229235858fd029d1dfd543901c6d05931f0bc2480a2d72c3.
//
// Solidity: event TeleporterAddressUnpaused(address indexed teleporterAddress)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerFilterer) WatchTeleporterAddressUnpaused(opts *bind.WatchOpts, sink chan<- *BatchCrossChainMessengerTeleporterAddressUnpaused, teleporterAddress []common.Address) (event.Subscription, error) {

	var teleporterAddressRule []interface{}
	for _, teleporterAddressItem := range teleporterAddress {
		teleporterAddressRule = append(teleporterAddressRule, teleporterAddressItem)
	}

	logs, sub, err := _BatchCrossChainMessenger.contract.WatchLogs(opts, "TeleporterAddressUnpaused", teleporterAddressRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(BatchCrossChainMessengerTeleporterAddressUnpaused)
				if err := _BatchCrossChainMessenger.contract.UnpackLog(event, "TeleporterAddressUnpaused", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTeleporterAddressUnpaused is a log parse operation binding the contract event 0x844e2f3154214672229235858fd029d1dfd543901c6d05931f0bc2480a2d72c3.
//
// Solidity: event TeleporterAddressUnpaused(address indexed teleporterAddress)
func (_BatchCrossChainMessenger *BatchCrossChainMessengerFilterer) ParseTeleporterAddressUnpaused(log types.Log) (*BatchCrossChainMessengerTeleporterAddressUnpaused, error) {
	event := new(BatchCrossChainMessengerTeleporterAddressUnpaused)
	if err := _BatchCrossChainMessenger.contract.UnpackLog(event, "TeleporterAddressUnpaused", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
