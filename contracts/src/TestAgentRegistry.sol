// SPDX-License-Identifier: MIT
pragma solidity 0.8.37;

/// @title A local-chain stand-in for ERC-8004's identity registry (docs/commercial-8004.md I1).
/// @notice Only the calls p2-testnet agents makes, as Base Sepolia's registry (2.0.0) answers
/// them (11/4, eth_call): register() mints the next agentId to the caller, whose agentWallet it
/// starts as; only the owner sets the URI or unsets the wallet ("Not authorized"). Not a whole
/// ERC-721: no transfers or approvals. Local anvil only: never deployed to a public chain (Base
/// Sepolia has the real one).
contract TestAgentRegistry {
    string public constant name = "AgentIdentity";
    string public constant symbol = "AGENT";

    uint256 internal last;
    mapping(uint256 => address) internal owners;
    mapping(uint256 => string) internal uris;
    mapping(uint256 => address) internal wallets;

    event Transfer(address indexed from, address indexed to, uint256 indexed tokenId);
    event Registered(uint256 indexed agentId, string agentURI, address indexed owner);
    event URIUpdated(uint256 indexed agentId, string newURI, address indexed updatedBy);

    modifier onlyOwner(uint256 agentId) {
        require(owners[agentId] == msg.sender, "Not authorized");
        _;
    }

    function register() external returns (uint256 agentId) {
        agentId = ++last;
        owners[agentId] = msg.sender;
        wallets[agentId] = msg.sender;
        emit Transfer(address(0), msg.sender, agentId);
        emit Registered(agentId, "", msg.sender);
    }

    function setAgentURI(uint256 agentId, string calldata newURI) external onlyOwner(agentId) {
        uris[agentId] = newURI;
        emit URIUpdated(agentId, newURI, msg.sender);
    }

    function unsetAgentWallet(uint256 agentId) external onlyOwner(agentId) {
        wallets[agentId] = address(0);
    }

    /// @notice As the registry's 2.0.0 (docs/commercial-8004.md §8): the owner sets the wallet on the new
    /// wallet's EIP-712 signature over AgentWalletSet, in the domain ERC8004IdentityRegistry/1, its deadline
    /// at most 5 minutes ahead. EOA signatures only (no ERC-1271 here).
    bytes32 internal constant AGENT_WALLET_SET_TYPEHASH =
        keccak256("AgentWalletSet(uint256 agentId,address newWallet,address owner,uint256 deadline)");
    uint256 internal constant MAX_DEADLINE_DELAY = 5 minutes;

    function setAgentWallet(uint256 agentId, address newWallet, uint256 deadline, bytes calldata signature)
        external
        onlyOwner(agentId)
    {
        require(newWallet != address(0), "bad wallet");
        require(block.timestamp <= deadline, "expired");
        require(deadline <= block.timestamp + MAX_DEADLINE_DELAY, "deadline too far");
        bytes32 domain = keccak256(
            abi.encode(
                keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"),
                keccak256("ERC8004IdentityRegistry"),
                keccak256("1"),
                block.chainid,
                address(this)
            )
        );
        bytes32 digest = keccak256(
            abi.encodePacked(
                "\x19\x01",
                domain,
                keccak256(abi.encode(AGENT_WALLET_SET_TYPEHASH, agentId, newWallet, owners[agentId], deadline))
            )
        );
        require(signature.length == 65, "invalid wallet sig");
        bytes32 r = bytes32(signature[0:32]);
        bytes32 s = bytes32(signature[32:64]);
        uint8 v = uint8(signature[64]);
        require(ecrecover(digest, v, r, s) == newWallet, "invalid wallet sig");
        wallets[agentId] = newWallet;
    }

    function ownerOf(uint256 agentId) external view returns (address owner) {
        owner = owners[agentId];
        require(owner != address(0), "no such agent");
    }

    function tokenURI(uint256 agentId) external view returns (string memory) {
        require(owners[agentId] != address(0), "no such agent");
        return uris[agentId];
    }

    function getAgentWallet(uint256 agentId) external view returns (address) {
        return wallets[agentId];
    }
}
