// SPDX-License-Identifier: MIT
pragma solidity 0.8.37;

import {Budget, IVerifier} from "../src/Budget.sol";
import {Verifier} from "../src/Verifier.sol";
import {Fixtures as F} from "./Fixtures.sol";

/// The cheatcodes these tests use (no forge-std dependency).
interface Vm {
    function sign(uint256 privateKey, bytes32 digest) external pure returns (uint8 v, bytes32 r, bytes32 s);
    function addr(uint256 privateKey) external pure returns (address);
    function prank(address sender) external;
    function expectRevert(bytes4 selector) external;
    function expectEmit(bool, bool, bool, bool) external;
    function store(address target, bytes32 slot, bytes32 value) external;
}

/// Z2's contract tests (docs/commercial-z2.md): synthetic data and test
/// keys; proofs come from zk/setup through Fixtures.sol.
contract BudgetTest {
    Vm internal constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));
    uint256 internal constant ISSUER_KEY = 0xA11CE;
    uint256 internal constant POLICY_KEY = 0xB0B;
    uint256 internal constant OTHER_KEY = 0xC0FFEE;
    uint256 internal constant R = 21888242871839275222246405745257275088548364400416034343698204186575808495617;

    event Spent(uint256 indexed task, bytes32 intentHash, uint256 amount, uint256 nullifier, uint256 cNew);

    Verifier internal verifier;
    Budget internal budget;

    function setUp() public {
        verifier = new Verifier(); // nonce 1
        budget = new Budget(IVerifier(address(verifier)), vm.addr(ISSUER_KEY), vm.addr(POLICY_KEY)); // nonce 2
        require(address(budget) == F.BUDGET, "the fixtures were made for another address");
        require(block.chainid == F.CHAIN, "the fixtures were made for another chain");
    }

    // ---- helpers ----

    function register(string memory taskId, uint256 c0) internal {
        vm.prank(vm.addr(ISSUER_KEY));
        budget.registerTask(taskId, c0);
    }

    function sig(uint256 key, string memory taskId, bytes32 intent, uint256 amount, uint256 n, uint256 cNew)
        internal
        view
        returns (bytes memory)
    {
        bytes32 d = budget.policyDigest(budget.taskField(taskId), intent, amount, n, cNew);
        (uint8 v, bytes32 r, bytes32 s) =
            vm.sign(key, keccak256(abi.encodePacked("\x19Ethereum Signed Message:\n32", d)));
        return abi.encodePacked(r, s, v);
    }

    struct S {
        string task;
        uint256 cOld;
        uint256 amount;
        uint256 cNew;
        uint256 n;
        bytes32 intent;
        bytes proof;
    }

    function a1() internal pure returns (S memory) {
        return S("show-a-01", F.A1_C_OLD, F.A1_AMOUNT, F.A1_C_NEW, F.A1_N, F.A1_INTENT, F.A1_PROOF);
    }

    function a2() internal pure returns (S memory) {
        return S("show-a-01", F.A2_C_OLD, F.A2_AMOUNT, F.A2_C_NEW, F.A2_N, F.A2_INTENT, F.A2_PROOF);
    }

    function a1b() internal pure returns (S memory) {
        return S("show-a-01", F.A1B_C_OLD, F.A1B_AMOUNT, F.A1B_C_NEW, F.A1B_N, F.A1B_INTENT, F.A1B_PROOF);
    }

    function b1() internal pure returns (S memory) {
        return S("show-b-01", F.B1_C_OLD, F.B1_AMOUNT, F.B1_C_NEW, F.B1_N, F.B1_INTENT, F.B1_PROOF);
    }

    function send(S memory x, bytes memory policySig) internal {
        budget.spend(x.task, x.cOld, x.amount, x.cNew, x.n, x.intent, x.proof, policySig);
    }

    function send(S memory x) internal {
        send(x, signed(x));
    }

    /// The policy signature for x. Compute it before vm.prank, expectRevert
    /// or expectEmit: they apply to the next call, and signing calls the
    /// contract's view functions.
    function signed(S memory x) internal view returns (bytes memory) {
        return sig(POLICY_KEY, x.task, x.intent, x.amount, x.n, x.cNew);
    }

    function slotOf(uint256 task) internal pure returns (bytes32) {
        return keccak256(abi.encode(task, uint256(0))); // commitment is slot 0
    }

    function unchanged(string memory taskId, uint256 c, uint256 n) internal view {
        require(budget.commitment(budget.taskField(taskId)) == c, "the commitment changed");
        require(!budget.nullifierUsed(n), "the nullifier was used");
    }

    // ---- the closed loop ----

    function test_RegisterAndSpendTwice() public {
        register("show-a-01", F.C0_A);
        S memory x = a1();
        bytes memory ps = signed(x);
        uint256 task = budget.taskField(x.task);
        vm.expectEmit(true, false, false, true);
        emit Spent(task, x.intent, x.amount, x.n, x.cNew);
        send(x, ps);
        require(budget.commitment(budget.taskField(x.task)) == F.A1_C_NEW && budget.nullifierUsed(F.A1_N), "after A1");
        send(a2());
        require(budget.commitment(budget.taskField(x.task)) == F.A2_C_NEW && budget.nullifierUsed(F.A2_N), "after A2");
    }

    function test_AnyoneMaySubmitASignedSpend() public {
        register("show-a-01", F.C0_A);
        bytes memory ps = signed(a1());
        vm.prank(address(0xBEEF)); // a relayer: the authority is the policy signature, not the sender
        send(a1(), ps);
        require(budget.nullifierUsed(F.A1_N), "relayed spend");
    }

    // ---- registration ----

    function test_RegisterOnlyIssuer() public {
        vm.expectRevert(Budget.NotIssuer.selector);
        budget.registerTask("show-a-01", F.C0_A);
    }

    function test_RegisterOnce() public {
        register("show-a-01", F.C0_A);
        vm.prank(vm.addr(ISSUER_KEY));
        vm.expectRevert(Budget.AlreadyRegistered.selector);
        budget.registerTask("show-a-01", F.C0_B);
    }

    function test_RegisterRefusesAnInvalidCommitment() public {
        vm.prank(vm.addr(ISSUER_KEY));
        vm.expectRevert(Budget.InvalidCommitment.selector);
        budget.registerTask("show-a-01", R);
    }

    // ---- what must be refused, with nothing written ----

    function test_ZeroAmount() public {
        register("show-a-01", F.C0_A);
        S memory x = a1();
        x.amount = 0;
        bytes memory ps = signed(x);
        vm.expectRevert(Budget.ZeroAmount.selector);
        send(x, ps);
    }

    function test_UnregisteredTask() public {
        bytes memory ps = signed(a1());
        vm.expectRevert(Budget.StaleCommitment.selector);
        send(a1(), ps);
    }

    function test_Replay() public {
        register("show-a-01", F.C0_A);
        send(a1());
        bytes memory ps = signed(a1());
        vm.expectRevert(Budget.StaleCommitment.selector);
        send(a1(), ps);
    }

    function test_SecondSpendOfAnOldState() public {
        register("show-a-01", F.C0_A);
        send(a1());
        bytes memory ps = signed(a1b());
        vm.expectRevert(Budget.StaleCommitment.selector);
        send(a1b(), ps);
    }

    function test_UsedNullifierWithTheCommitmentReinstated() public {
        register("show-a-01", F.C0_A);
        send(a1());
        vm.store(address(budget), slotOf(budget.taskField("show-a-01")), bytes32(F.C0_A));
        bytes memory ps = signed(a1());
        vm.expectRevert(Budget.NullifierUsed.selector);
        send(a1(), ps);
    }

    function test_TamperedAmount() public {
        register("show-a-01", F.C0_A);
        S memory x = a1();
        x.amount += 1; // signed by the policy layer too: only the proof can refuse it
        bytes memory ps = signed(x);
        vm.expectRevert(Verifier.ProofInvalid.selector);
        send(x, ps);
        unchanged(x.task, F.C0_A, F.A1_N);
    }

    function test_TamperedNewCommitment() public {
        register("show-a-01", F.C0_A);
        S memory x = a1();
        x.cNew = F.A2_C_NEW;
        bytes memory ps = signed(x);
        vm.expectRevert(Verifier.ProofInvalid.selector);
        send(x, ps);
        unchanged(x.task, F.C0_A, F.A1_N);
    }

    function test_TamperedNullifier() public {
        register("show-a-01", F.C0_A);
        S memory x = a1();
        x.n = F.A2_N;
        bytes memory ps = signed(x);
        vm.expectRevert(Verifier.ProofInvalid.selector);
        send(x, ps);
        unchanged(x.task, F.C0_A, F.A2_N);
    }

    function test_TamperedIntentHi() public {
        register("show-a-01", F.C0_A);
        S memory x = a1();
        x.intent = x.intent ^ bytes32(uint256(1) << 200);
        bytes memory ps = signed(x);
        vm.expectRevert(Verifier.ProofInvalid.selector);
        send(x, ps);
        unchanged(x.task, F.C0_A, F.A1_N);
    }

    function test_TamperedIntentLo() public {
        register("show-a-01", F.C0_A);
        S memory x = a1();
        x.intent = x.intent ^ bytes32(uint256(1));
        bytes memory ps = signed(x);
        vm.expectRevert(Verifier.ProofInvalid.selector);
        send(x, ps);
        unchanged(x.task, F.C0_A, F.A1_N);
    }

    function test_TamperedProof() public {
        register("show-a-01", F.C0_A);
        S memory x = a1();
        x.proof[200] = x.proof[200] ^ 0x01;
        bytes memory ps = signed(x);
        vm.expectRevert(Verifier.ProofInvalid.selector);
        send(x, ps);
        unchanged(x.task, F.C0_A, F.A1_N);
    }

    /// A's proof spent as B's: B holds A's current commitment (planted), so
    /// only the task binding inside the proof refuses it.
    function test_AnotherTasksProof() public {
        register("show-b-01", F.C0_B);
        vm.store(address(budget), slotOf(budget.taskField("show-b-01")), bytes32(F.A1_C_OLD));
        S memory x = a1();
        x.task = "show-b-01";
        bytes memory ps = signed(x);
        vm.expectRevert(Verifier.ProofInvalid.selector);
        send(x, ps);
        unchanged("show-b-01", F.A1_C_OLD, F.A1_N);
    }

    /// Cross-consumer: B's policy authorization does not spend A's task.
    function test_AnotherConsumersAuthorization() public {
        register("show-a-01", F.C0_A);
        register("show-b-01", F.C0_B);
        S memory x = a1();
        bytes memory forB = sig(POLICY_KEY, "show-b-01", x.intent, x.amount, x.n, x.cNew);
        vm.expectRevert(Budget.PolicySignatureInvalid.selector);
        send(x, forB);
        unchanged(x.task, F.C0_A, F.A1_N);
        send(b1()); // B's own spend goes through
    }

    /// The same proof on another deployment: its task field differs.
    function test_AnotherDeployment() public {
        Budget other = new Budget(IVerifier(address(verifier)), vm.addr(ISSUER_KEY), vm.addr(POLICY_KEY));
        vm.prank(vm.addr(ISSUER_KEY));
        other.registerTask("show-a-01", F.C0_A);
        S memory x = a1();
        bytes32 d = other.policyDigest(other.taskField(x.task), x.intent, x.amount, x.n, x.cNew);
        (uint8 v, bytes32 r, bytes32 s) =
            vm.sign(POLICY_KEY, keccak256(abi.encodePacked("\x19Ethereum Signed Message:\n32", d)));
        vm.expectRevert(Verifier.ProofInvalid.selector);
        other.spend(x.task, x.cOld, x.amount, x.cNew, x.n, x.intent, x.proof, abi.encodePacked(r, s, v));
    }

    // ---- the policy layer (Z2-D1) ----

    function test_NoPolicySignature() public {
        register("show-a-01", F.C0_A);
        vm.expectRevert(Budget.PolicySignatureInvalid.selector);
        send(a1(), "");
    }

    function test_PolicySignatureByAnotherKey() public {
        register("show-a-01", F.C0_A);
        S memory x = a1();
        bytes memory ps = sig(OTHER_KEY, x.task, x.intent, x.amount, x.n, x.cNew);
        vm.expectRevert(Budget.PolicySignatureInvalid.selector);
        send(x, ps);
    }

    function test_PolicySignatureForAnotherAmount() public {
        register("show-a-01", F.C0_A);
        S memory x = a1();
        bytes memory ps = sig(POLICY_KEY, x.task, x.intent, x.amount + 1, x.n, x.cNew);
        vm.expectRevert(Budget.PolicySignatureInvalid.selector);
        send(x, ps);
    }

    function test_PolicySignatureForAnotherIntent() public {
        register("show-a-01", F.C0_A);
        S memory x = a1();
        bytes memory ps = sig(POLICY_KEY, x.task, F.A2_INTENT, x.amount, x.n, x.cNew);
        vm.expectRevert(Budget.PolicySignatureInvalid.selector);
        send(x, ps);
    }

    function test_MalleatedPolicySignature() public {
        register("show-a-01", F.C0_A);
        S memory x = a1();
        bytes memory good = sig(POLICY_KEY, x.task, x.intent, x.amount, x.n, x.cNew);
        uint256 n = 0xfffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141;
        bytes32 r;
        bytes32 s;
        uint8 v = uint8(good[64]);
        assembly {
            r := mload(add(good, 32))
            s := mload(add(good, 64))
        }
        bytes memory flipped = abi.encodePacked(r, bytes32(n - uint256(s)), v == 27 ? uint8(28) : uint8(27));
        vm.expectRevert(Budget.PolicySignatureInvalid.selector);
        send(x, flipped);
    }

    // ---- public inputs must be canonical (Z1 finding 2) ----

    function test_NullifierPlusR() public {
        register("show-a-01", F.C0_A);
        S memory x = a1();
        x.n += R;
        bytes memory ps = signed(x);
        vm.expectRevert(Verifier.PublicInputNotInField.selector);
        send(x, ps);
    }

    function test_NewCommitmentPlusR() public {
        register("show-a-01", F.C0_A);
        S memory x = a1();
        x.cNew += R;
        bytes memory ps = signed(x);
        vm.expectRevert(Verifier.PublicInputNotInField.selector);
        send(x, ps);
    }

    // ---- a failed transaction, then the resend ----

    function test_FailedThenResent() public {
        register("show-a-01", F.C0_A);
        S memory bad = a1();
        bad.amount += 1;
        try this.spendFor(bad) {
            revert("a tampered spend went through");
        } catch {}
        unchanged("show-a-01", F.C0_A, F.A1_N);
        send(a1());
        require(budget.commitment(budget.taskField("show-a-01")) == F.A1_C_NEW, "the resend did not apply");
        bytes memory ps = signed(a1());
        vm.expectRevert(Budget.StaleCommitment.selector);
        send(a1(), ps);
    }

    function spendFor(S memory x) external {
        send(x);
    }
}
