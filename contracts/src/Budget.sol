// SPDX-License-Identifier: MIT
pragma solidity 0.8.37;

/// The Groth16 verifier generated from Z2's setup (zk/setup/Verifier.sol).
interface IVerifier {
    function verifyProof(bytes calldata proof, uint256[7] calldata input) external view;
}

/// @title Z2 budget contract (docs/commercial-z2.md); local chain only.
/// @notice Holds one commitment per task and spends it with a proof that
/// the balance covers the amount, without the balance. A spend needs, in
/// order: a non-zero amount (Z2-D4); the task's current commitment as
/// C_old; an unused nullifier; the policy layer's signature over the
/// spend (Z2-D1); and a valid proof over public inputs the contract builds
/// itself (the task field from chain, contract and task ID: Z2-D2; the two
/// intent limbs from intent_hash: Z1-D1). Then, in the same transaction,
/// the nullifier is used, C_new stored and Spent emitted.
/// @dev Synthetic data and test keys only. The setup is the host's own:
/// 信任等同信任主持人 (Z1-D4).
contract Budget {
    /// The BN254 scalar field modulus r.
    uint256 internal constant R = 21888242871839275222246405745257275088548364400416034343698204186575808495617;
    /// secp256k1's group order n / 2 (a lower s only: no malleated signature).
    uint256 internal constant HALF_N = 0x7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a0;
    bytes internal constant TASK_LABEL = "agent-payment-authority/zk/v2/task";
    bytes32 internal constant POLICY_LABEL = keccak256("agent-payment-authority/zk/v2/policy");

    IVerifier public immutable verifier;
    address public immutable issuer;
    address public immutable policySigner;

    /// task field => current commitment (0: not registered).
    mapping(uint256 => uint256) public commitment;
    mapping(uint256 => bool) public nullifierUsed;

    event TaskRegistered(uint256 indexed task, string taskId, uint256 commitment);
    event Spent(uint256 indexed task, bytes32 intentHash, uint256 amount, uint256 nullifier, uint256 cNew);

    error NotIssuer();
    error AlreadyRegistered();
    error InvalidCommitment();
    error StaleCommitment();
    error NullifierUsed();
    error ZeroAmount();
    error PolicySignatureInvalid();

    constructor(IVerifier verifier_, address issuer_, address policySigner_) {
        require(address(verifier_) != address(0) && issuer_ != address(0) && policySigner_ != address(0), "zero address");
        verifier = verifier_;
        issuer = issuer_;
        policySigner = policySigner_;
    }

    /// The task's field element on this deployment: the first 31 bytes of
    /// SHA-256(TASK_LABEL ‖ chain_id ‖ this contract ‖ task_id).
    function taskField(string calldata taskId) public view returns (uint256) {
        return uint256(sha256(abi.encodePacked(TASK_LABEL, block.chainid, address(this), taskId))) >> 8;
    }

    /// The issuer registers a task's first commitment, once.
    function registerTask(string calldata taskId, uint256 c0) external {
        if (msg.sender != issuer) revert NotIssuer();
        uint256 task = taskField(taskId);
        if (commitment[task] != 0) revert AlreadyRegistered();
        if (c0 == 0 || c0 >= R) revert InvalidCommitment();
        commitment[task] = c0;
        emit TaskRegistered(task, taskId, c0);
    }

    /// What the policy layer signs (EIP-191 over this digest): the spend
    /// on this chain and contract, its intent and amount, the old state's
    /// nullifier and the new commitment.
    function policyDigest(uint256 task, bytes32 intentHash, uint256 amount, uint256 nullifier, uint256 cNew)
        public
        view
        returns (bytes32)
    {
        return keccak256(abi.encode(POLICY_LABEL, block.chainid, address(this), task, intentHash, amount, nullifier, cNew));
    }

    function spend(
        string calldata taskId,
        uint256 cOld,
        uint256 amount,
        uint256 cNew,
        uint256 nullifier,
        bytes32 intentHash,
        bytes calldata proof,
        bytes calldata policySig
    ) external {
        if (amount == 0) revert ZeroAmount();
        uint256 task = taskField(taskId);
        uint256 current = commitment[task];
        if (current == 0 || current != cOld) revert StaleCommitment();
        if (nullifierUsed[nullifier]) revert NullifierUsed();
        if (cNew == 0) revert InvalidCommitment();
        if (signer(policyDigest(task, intentHash, amount, nullifier, cNew), policySig) != policySigner) {
            revert PolicySignatureInvalid();
        }
        // The verifier refuses any input not below r, so every value used
        // below is the canonical one it verified.
        verifier.verifyProof(
            proof, [task, cOld, amount, cNew, nullifier, uint256(intentHash) >> 128, uint256(intentHash) & type(uint128).max]
        );
        nullifierUsed[nullifier] = true;
        commitment[task] = cNew;
        emit Spent(task, intentHash, amount, nullifier, cNew);
    }

    /// The EIP-191 signer of digest, or address(0) for a malformed or
    /// malleated signature.
    function signer(bytes32 digest, bytes calldata sig) internal pure returns (address) {
        if (sig.length != 65) return address(0);
        bytes32 r = bytes32(sig[0:32]);
        bytes32 s = bytes32(sig[32:64]);
        uint8 v = uint8(sig[64]);
        if (uint256(s) > HALF_N || (v != 27 && v != 28)) return address(0);
        return ecrecover(keccak256(abi.encodePacked("\x19Ethereum Signed Message:\n32", digest)), v, r, s);
    }
}
