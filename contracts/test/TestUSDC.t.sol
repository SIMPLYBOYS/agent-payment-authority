// SPDX-License-Identifier: MIT
pragma solidity 0.8.37;

import {TestUSDC} from "../src/TestUSDC.sol";

interface Vm {
    function sign(uint256 privateKey, bytes32 digest) external pure returns (uint8 v, bytes32 r, bytes32 s);
    function addr(uint256 privateKey) external pure returns (address);
    function prank(address sender) external;
    function expectRevert(bytes4 selector) external;
    function warp(uint256 newTimestamp) external;
}

/// The local USDC stand-in's EIP-3009 (docs/commercial-p2.md, P2-a).
contract TestUSDCTest {
    Vm internal constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));
    uint256 internal constant PAYER_KEY = 0xA;
    uint256 internal constant OTHER_KEY = 0xB;
    address internal constant MERCHANT = address(0x3333);
    TestUSDC internal usdc;
    address internal payer;

    function setUp() public {
        usdc = new TestUSDC();
        payer = vm.addr(PAYER_KEY);
        usdc.mint(payer, 5_000_000);
        vm.warp(1000);
    }

    function auth(uint256 key, address to, uint256 value, uint256 after_, uint256 before, bytes32 nonce)
        internal
        view
        returns (uint8 v, bytes32 r, bytes32 s)
    {
        bytes32 h = keccak256(abi.encode(usdc.TRANSFER_WITH_AUTHORIZATION_TYPEHASH(), payer, to, value, after_, before, nonce));
        return vm.sign(key, keccak256(abi.encodePacked("\x19\x01", usdc.DOMAIN_SEPARATOR(), h)));
    }

    function pay(uint256 key, address to, uint256 value, uint256 after_, uint256 before, bytes32 nonce) internal {
        (uint8 v, bytes32 r, bytes32 s) = auth(key, to, value, after_, before, nonce);
        usdc.transferWithAuthorization(payer, to, value, after_, before, nonce, v, r, s);
    }

    function test_Pays() public {
        pay(PAYER_KEY, MERCHANT, 500_000, 0, 2000, bytes32(uint256(1)));
        require(usdc.balanceOf(MERCHANT) == 500_000 && usdc.balanceOf(payer) == 4_500_000, "balances");
        require(usdc.authorizationState(payer, bytes32(uint256(1))), "nonce used");
    }

    function test_Replay() public {
        (uint8 v, bytes32 r, bytes32 s) = auth(PAYER_KEY, MERCHANT, 500_000, 0, 2000, bytes32(uint256(1)));
        usdc.transferWithAuthorization(payer, MERCHANT, 500_000, 0, 2000, bytes32(uint256(1)), v, r, s);
        vm.expectRevert(TestUSDC.AuthorizationUsedAlready.selector);
        usdc.transferWithAuthorization(payer, MERCHANT, 500_000, 0, 2000, bytes32(uint256(1)), v, r, s);
    }

    function test_Expired() public {
        (uint8 v, bytes32 r, bytes32 s) = auth(PAYER_KEY, MERCHANT, 1, 0, 1000, bytes32(uint256(2)));
        vm.expectRevert(TestUSDC.AuthorizationExpired.selector);
        usdc.transferWithAuthorization(payer, MERCHANT, 1, 0, 1000, bytes32(uint256(2)), v, r, s);
    }

    function test_NotYetValid() public {
        (uint8 v, bytes32 r, bytes32 s) = auth(PAYER_KEY, MERCHANT, 1, 1000, 2000, bytes32(uint256(3)));
        vm.expectRevert(TestUSDC.AuthorizationNotYetValid.selector);
        usdc.transferWithAuthorization(payer, MERCHANT, 1, 1000, 2000, bytes32(uint256(3)), v, r, s);
    }

    function test_AnotherSigner() public {
        (uint8 v, bytes32 r, bytes32 s) = auth(OTHER_KEY, MERCHANT, 1, 0, 2000, bytes32(uint256(4)));
        vm.expectRevert(TestUSDC.InvalidSignature.selector);
        usdc.transferWithAuthorization(payer, MERCHANT, 1, 0, 2000, bytes32(uint256(4)), v, r, s);
    }

    /// Signed for the merchant, redirected elsewhere: refused.
    function test_AnotherRecipient() public {
        (uint8 v, bytes32 r, bytes32 s) = auth(PAYER_KEY, MERCHANT, 1, 0, 2000, bytes32(uint256(5)));
        vm.expectRevert(TestUSDC.InvalidSignature.selector);
        usdc.transferWithAuthorization(payer, address(0xBAD), 1, 0, 2000, bytes32(uint256(5)), v, r, s);
    }

    function test_AnotherAmount() public {
        (uint8 v, bytes32 r, bytes32 s) = auth(PAYER_KEY, MERCHANT, 1, 0, 2000, bytes32(uint256(6)));
        vm.expectRevert(TestUSDC.InvalidSignature.selector);
        usdc.transferWithAuthorization(payer, MERCHANT, 2, 0, 2000, bytes32(uint256(6)), v, r, s);
    }

    function test_Malleated() public {
        (uint8 v, bytes32 r, bytes32 s) = auth(PAYER_KEY, MERCHANT, 1, 0, 2000, bytes32(uint256(7)));
        bytes32 high = bytes32(0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141 - uint256(s));
        vm.expectRevert(TestUSDC.InvalidSignature.selector);
        usdc.transferWithAuthorization(payer, MERCHANT, 1, 0, 2000, bytes32(uint256(7)), v == 27 ? 28 : 27, r, high);
    }

    function test_InsufficientBalance() public {
        (uint8 v, bytes32 r, bytes32 s) = auth(PAYER_KEY, MERCHANT, 6_000_000, 0, 2000, bytes32(uint256(8)));
        vm.expectRevert(TestUSDC.InsufficientBalance.selector);
        usdc.transferWithAuthorization(payer, MERCHANT, 6_000_000, 0, 2000, bytes32(uint256(8)), v, r, s);
        require(!usdc.authorizationState(payer, bytes32(uint256(8))), "a failed transfer used the nonce");
    }

    function test_MintOnlyOwner() public {
        vm.prank(payer);
        vm.expectRevert(TestUSDC.NotOwner.selector);
        usdc.mint(payer, 1);
    }
}
