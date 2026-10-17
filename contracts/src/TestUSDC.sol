// SPDX-License-Identifier: MIT
pragma solidity 0.8.37;

/// @title A local-chain stand-in for USDC (docs/commercial-p2.md, P2-a).
/// @notice ERC-20 balances and EIP-3009 transferWithAuthorization under the
/// EIP-712 domain ("USDC", "2"), as the payment Signer signs for. Only the
/// owner mints. Local anvil only: never deployed to a public chain (Base
/// Sepolia has its own USDC).
contract TestUSDC {
    string public constant name = "USDC";
    string public constant version = "2";
    uint8 public constant decimals = 6;
    /// secp256k1's group order n / 2 (a lower s only: no malleated signature).
    uint256 internal constant HALF_N = 0x7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a0;
    bytes32 public constant TRANSFER_WITH_AUTHORIZATION_TYPEHASH = keccak256(
        "TransferWithAuthorization(address from,address to,uint256 value,uint256 validAfter,uint256 validBefore,bytes32 nonce)"
    );

    address public immutable owner;
    uint256 public totalSupply;
    mapping(address => uint256) public balanceOf;
    /// authorizer => nonce => used
    mapping(address => mapping(bytes32 => bool)) public authorizationState;

    event Transfer(address indexed from, address indexed to, uint256 value);
    event AuthorizationUsed(address indexed authorizer, bytes32 indexed nonce);

    error NotOwner();
    error InsufficientBalance();
    error AuthorizationNotYetValid();
    error AuthorizationExpired();
    error AuthorizationUsedAlready();
    error InvalidSignature();
    error ZeroAddress();

    constructor() {
        owner = msg.sender;
    }

    function DOMAIN_SEPARATOR() public view returns (bytes32) {
        return keccak256(
            abi.encode(
                keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"),
                keccak256(bytes(name)),
                keccak256(bytes(version)),
                block.chainid,
                address(this)
            )
        );
    }

    function mint(address to, uint256 value) external {
        if (msg.sender != owner) revert NotOwner();
        if (to == address(0)) revert ZeroAddress();
        totalSupply += value;
        balanceOf[to] += value;
        emit Transfer(address(0), to, value);
    }

    function transfer(address to, uint256 value) external returns (bool) {
        _transfer(msg.sender, to, value);
        return true;
    }

    /// EIP-3009: anyone may submit; the authority is from's signature.
    function transferWithAuthorization(
        address from,
        address to,
        uint256 value,
        uint256 validAfter,
        uint256 validBefore,
        bytes32 nonce,
        uint8 v,
        bytes32 r,
        bytes32 s
    ) external {
        if (block.timestamp <= validAfter) revert AuthorizationNotYetValid();
        if (block.timestamp >= validBefore) revert AuthorizationExpired();
        if (authorizationState[from][nonce]) revert AuthorizationUsedAlready();
        bytes32 digest = keccak256(
            abi.encodePacked(
                "\x19\x01",
                DOMAIN_SEPARATOR(),
                keccak256(abi.encode(TRANSFER_WITH_AUTHORIZATION_TYPEHASH, from, to, value, validAfter, validBefore, nonce))
            )
        );
        if (uint256(s) > HALF_N || (v != 27 && v != 28)) revert InvalidSignature();
        address signer = ecrecover(digest, v, r, s);
        if (signer == address(0) || signer != from) revert InvalidSignature();
        authorizationState[from][nonce] = true;
        emit AuthorizationUsed(from, nonce);
        _transfer(from, to, value);
    }

    function _transfer(address from, address to, uint256 value) internal {
        if (to == address(0)) revert ZeroAddress();
        if (balanceOf[from] < value) revert InsufficientBalance();
        balanceOf[from] -= value;
        balanceOf[to] += value;
        emit Transfer(from, to, value);
    }
}
