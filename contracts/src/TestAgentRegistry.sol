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
