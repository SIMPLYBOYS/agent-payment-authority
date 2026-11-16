import type { DesignSystem, Page, SlideMeta } from '@open-slide/core';
import { Step, Steps, useSlidePageNumber } from '@open-slide/core';
import type { CSSProperties, ReactNode } from 'react';

// Agent Payment Authority: the payment architecture, and what a real deployment has to change.
// Every claim is the project's own (docs/architecture.md, docs/positioning.md, docs/commercial-ekyc.md).

export const design: DesignSystem = {
  palette: { bg: '#fbf7ee', text: '#1c1a17', accent: '#d9512c' },
  fonts: {
    display: '"Songti TC", "Noto Serif TC", Georgia, serif',
    body: '-apple-system, "PingFang TC", "Noto Sans TC", system-ui, sans-serif',
  },
  typeScale: { hero: 118, body: 34 },
  radius: 14,
};

const muted = '#6f675c';
const line = '#d9d0bf';
const panel = '#f3ecdd';
const ink = '#1c1a17';
const green = '#2f7d4f';
const amber = '#b9770e';
const PAD = 110;

const fill: CSSProperties = {
  width: '100%',
  height: '100%',
  fontFamily: 'var(--osd-font-body)',
  background: 'var(--osd-bg)',
  color: 'var(--osd-text)',
  position: 'relative',
  boxSizing: 'border-box',
};

const Footer = () => {
  const { current, total } = useSlidePageNumber();
  return (
    <div
      style={{
        position: 'absolute',
        left: PAD,
        right: PAD,
        bottom: 44,
        display: 'flex',
        justifyContent: 'space-between',
        fontSize: 22,
        color: muted,
        letterSpacing: '0.06em',
      }}
    >
      <span>Agent Payment Authority · 付款架構</span>
      <span>
        {String(current).padStart(2, '0')} / {String(total).padStart(2, '0')}
      </span>
    </div>
  );
};

const Frame = ({ eyebrow, title, children }: { eyebrow: string; title: string; children: ReactNode }) => (
  <div style={{ ...fill, padding: `86px ${PAD}px 0` }}>
    <div style={{ fontSize: 24, color: 'var(--osd-accent)', letterSpacing: '0.22em', fontWeight: 700 }}>{eyebrow}</div>
    <h2
      style={{
        fontFamily: 'var(--osd-font-display)',
        fontSize: 66,
        fontWeight: 800,
        lineHeight: 1.2,
        margin: '16px 0 0',
      }}
    >
      {title}
    </h2>
    <div style={{ marginTop: 44 }}>{children}</div>
    <Footer />
  </div>
);

// ---------- 01 cover ----------
const Cover: Page = () => (
  <div style={{ ...fill, padding: `0 ${PAD + 30}px`, display: 'flex', flexDirection: 'column', justifyContent: 'center' }}>
    <div style={{ fontSize: 26, color: 'var(--osd-accent)', letterSpacing: '0.24em', fontWeight: 700 }}>
      AGENT PAYMENT AUTHORITY
    </div>
    <h1
      style={{
        fontFamily: 'var(--osd-font-display)',
        fontSize: 'var(--osd-size-hero)',
        fontWeight: 900,
        lineHeight: 1.18,
        margin: '36px 0 0',
      }}
    >
      AI 可以提出付款，
      <br />
      不能自己批准。
    </h1>
    <p style={{ fontSize: 40, color: muted, margin: '44px 0 0', lineHeight: 1.5 }}>
      付款架構，以及部署到實際場景要換掉的東西
    </p>
    <div style={{ marginTop: 72, fontSize: 24, color: muted, letterSpacing: '0.06em' }}>
      2026-11 · 依專案現況整理 · 測試網與沙盒，不是正式環境
    </div>
  </div>
);

// ---------- 02 the three worries ----------
const Worry = ({ n, title, text }: { n: string; title: string; text: string }) => (
  <div style={{ flex: 1, background: panel, borderRadius: 'var(--osd-radius)', padding: '44px 44px 48px' }}>
    <div style={{ fontFamily: 'var(--osd-font-display)', fontSize: 64, fontStyle: 'italic', color: 'var(--osd-accent)' }}>{n}</div>
    <div style={{ fontSize: 44, fontWeight: 800, marginTop: 20 }}>{title}</div>
    <div style={{ fontSize: 30, lineHeight: 1.55, color: muted, marginTop: 20 }}>{text}</div>
  </div>
);

const Worries: Page = () => (
  <Frame eyebrow="問題" title="讓 AI 替人花錢，要先回答三件事">
    <div style={{ display: 'flex', gap: 36, marginTop: 20 }}>
      <Worry n="01" title="被騙著亂花" text="商店頁面藏一句話，AI 就可能多買，或付給別人。" />
      <Worry n="02" title="個資被拿走" text="買一塊蛋糕，不該交出姓名、生日和證件號碼。" />
      <Worry n="03" title="出了事說不清楚" text="這筆錢是誰同意的、依哪條規則付的，要查得到。" />
    </div>
    <p style={{ fontSize: 34, lineHeight: 1.5, marginTop: 56 }}>
      錢包知道「花多少」，不知道「為了哪個任務、誰同意的」。這一層補的是後者。
    </p>
  </Frame>
);

// ---------- 03 the division of labour ----------
const Half = ({ tag, title, items, dark }: { tag: string; title: string; items: [string, string, string]; dark?: boolean }) => (
  <div
    style={{
      flex: 1,
      background: dark ? ink : panel,
      color: dark ? '#fbf7ee' : ink,
      borderRadius: 'var(--osd-radius)',
      padding: '48px 52px 52px',
    }}
  >
    <div style={{ fontSize: 24, letterSpacing: '0.2em', fontWeight: 700, color: dark ? '#f0a184' : 'var(--osd-accent)' }}>{tag}</div>
    <div style={{ fontFamily: 'var(--osd-font-display)', fontSize: 56, fontWeight: 800, marginTop: 16 }}>{title}</div>
    <ul style={{ fontSize: 32, lineHeight: 1.5, margin: '32px 0 0', paddingLeft: 36 }}>
      <li style={{ marginBottom: 14 }}>{items[0]}</li>
      <li style={{ marginBottom: 14 }}>{items[1]}</li>
      <li>{items[2]}</li>
    </ul>
  </div>
);

const Split: Page = () => (
  <Frame eyebrow="分工" title="x402 管「怎麼付」，這一層管「能不能付」">
    <div style={{ display: 'flex', gap: 36, marginTop: 12 }}>
      <Half
        tag="X402 V2"
        title="怎麼付"
        items={['商店用 HTTP 402 回覆報價', '付款方簽一份授權', 'facilitator 上鏈結算']}
      />
      <Half
        dark
        tag="AGENT PAYMENT AUTHORITY"
        title="能不能付"
        items={['這筆屬於哪個任務、哪份委託', '依哪條規則放行，誰核准的', '事後任何人都能向鏈重算']}
      />
    </div>
    <p style={{ fontSize: 30, color: muted, marginTop: 44, lineHeight: 1.5 }}>
      x402 本身沒有預算、上限、本人核准或本人身分的概念；換成別的付款方式，這一層的規則照樣成立。
    </p>
  </Frame>
);

// ---------- 04 standards: which layers follow one, which are still being written (docs/positioning.md §4.1) ----------
const Std = ({ layer, std, here, own }: { layer: string; std: string; here: string; own?: boolean }) => (
  <tr>
    <td style={{ ...cell, fontWeight: 800, width: 250 }}>{layer}</td>
    <td style={{ ...cell, width: 820 }}>{std}</td>
    <td style={{ ...cell, fontWeight: own ? 800 : 400, color: own ? 'var(--osd-accent)' : ink }}>{here}</td>
  </tr>
);

const Standards: Page = () => (
  <Frame eyebrow="標準" title="能用標準的地方都用標準，委託這一層還在制定">
    <table style={{ width: '100%', borderCollapse: 'collapse' }}>
      <thead>
        <tr>
          <th style={head}>層</th>
          <th style={head}>標準與現況</th>
          <th style={head}>這裡用的</th>
        </tr>
      </thead>
      <tbody>
        <Std layer="付款協商" std="x402 v2，由 Linux Foundation 的 x402 Foundation 治理" here="照用（exact）" />
        <Std layer="簽名與結算" std="EIP-3009＋EIP-712，USDC 有實作" here="照用" />
        <Std layer="本人核准" std="WebAuthn／FIDO2，W3C 與 FIDO 的標準" here="照用（passkey）" />
        <Std layer="Agent 身分" std="ERC-8004，仍是草案，2026/1 上以太坊主網" here="照用（Base Sepolia）" />
        <Std layer="選擇性揭露" std="W3C VC 2.0；SD-JWT 或 BBS（bbs-2023）" here="自訂信封、舊版 BBS+" own />
        <Std layer="委託與核准" std="AP2 v0.2、Verifiable Intent v0.1，都在 FIDO 審" here="自訂格式，設計對齊" own />
      </tbody>
    </table>
    <div style={{ marginTop: 30, background: ink, color: '#fbf7ee', borderRadius: 'var(--osd-radius)', padding: '24px 40px', fontSize: 28, lineHeight: 1.5 }}>
      模式對得上：Allow 就是草案裡的 autonomous，Ask 就是本人在場的核准。
      <br />
      最大的差別：草案給 Agent 一把金鑰簽這一筆；這裡的 Agent 什麼金鑰都沒有。
    </div>
  </Frame>
);

// ---------- 04 roles: who talks to whom ----------
const Box = ({
  x, y, w, h, title, sub, dark,
}: { x: number; y: number; w: number; h: number; title: string; sub: string; dark?: boolean }) => (
  <div
    style={{
      position: 'absolute',
      left: x,
      top: y,
      width: w,
      height: h,
      boxSizing: 'border-box',
      background: dark ? ink : panel,
      color: dark ? '#fbf7ee' : ink,
      border: `2px solid ${dark ? ink : line}`,
      borderRadius: 'var(--osd-radius)',
      padding: '18px 24px',
      display: 'flex',
      flexDirection: 'column',
      justifyContent: 'center',
    }}
  >
    <div style={{ fontSize: 32, fontWeight: 800 }}>{title}</div>
    <div style={{ fontSize: 24, lineHeight: 1.4, marginTop: 6, color: dark ? '#d8cfbf' : muted }}>{sub}</div>
  </div>
);

const Arrow = ({ d, label, lx, ly, dashed }: { d: string; label?: string; lx?: number; ly?: number; dashed?: boolean }) => (
  <g>
    <path d={d} fill="none" stroke={dashed ? muted : '#d9512c'} strokeWidth={3} strokeDasharray={dashed ? '10 8' : undefined} markerEnd={dashed ? 'url(#arrow-muted)' : 'url(#arrow)'} />
    {label ? (
      <text x={lx} y={ly} fontSize={22} fill={muted} fontFamily="-apple-system, PingFang TC, sans-serif">
        {label}
      </text>
    ) : null}
  </g>
);

const Roles: Page = () => (
  <Frame eyebrow="角色" title="權力拆給互不相通的角色">
    <div style={{ position: 'relative', width: 1700, height: 690 }}>
      <svg width={1700} height={690} style={{ position: 'absolute', left: 0, top: 0 }} aria-hidden="true">
        <defs>
          <marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse">
            <path d="M0 0 L10 5 L0 10 z" fill="#d9512c" />
          </marker>
          <marker id="arrow-muted" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse">
            <path d="M0 0 L10 5 L0 10 z" fill={muted} />
          </marker>
        </defs>
        <Arrow d="M340 75 L520 75" label="確認・核准" lx={374} ly={58} />
        <Arrow d="M340 420 L520 420" label="提出請購" lx={380} ly={404} />
        <Arrow d="M730 300 L730 160" label="請求簽名" lx={746} ly={238} />
        <Arrow d="M940 380 L1080 250" label="tx#1" lx={1024} ly={352} />
        <Arrow d="M940 460 L1080 520" label="tx#2" lx={952} ly={524} />
        <Arrow d="M730 540 L730 620 L1590 620 L1590 454" label="證據包" lx={1110} ly={654} dashed />
        <Arrow d="M1440 215 L1478 292" dashed />
        <Arrow d="M1440 505 L1478 410" dashed />
      </svg>
      <Box x={0} y={10} w={340} h={130} title="本人" sub="已登記的裝置（WebAuthn）" />
      <Box x={0} y={355} w={340} h={130} title="Agent（不受信任）" sub="只能看商品、取報價、提出請購" />
      <Box x={520} y={10} w={420} h={150} dark title="Signer（獨立帳號）" sub="錢包私鑰；只簽委託範圍內的商家與金額" />
      <Box x={520} y={300} w={420} h={240} title="Host" sub="Policy：Deny / Ask / Allow　Store：事件雜湊鏈　憑證驗證：BBS+ 選擇性揭露　沒有私鑰" />
      <Box x={1080} y={150} w={360} h={130} title="Relayer・預算合約" sub="ZK 預算預留（Groth16）" />
      <Box x={1080} y={440} w={360} h={130} title="Facilitator・商店" sub="x402・EIP-3009 結算" />
      <Box x={1480} y={250} w={220} h={200} title="Checker" sub="只讀證據包與鏈，獨立判定" />
    </div>
  </Frame>
);

// ---------- 05 who can do what ----------
const cell: CSSProperties = { fontSize: 29, lineHeight: 1.45, padding: '18px 24px 18px 0', verticalAlign: 'top', borderTop: `2px solid ${line}` };
const head: CSSProperties = { fontSize: 24, letterSpacing: '0.14em', color: muted, textAlign: 'left', padding: '0 24px 14px 0', fontWeight: 700 };

const Power = ({ who, can, cannot }: { who: string; can: string; cannot: string }) => (
  <tr>
    <td style={{ ...cell, fontWeight: 800, width: 230 }}>{who}</td>
    <td style={{ ...cell, width: 720 }}>{can}</td>
    <td style={{ ...cell, color: 'var(--osd-accent)' }}>{cannot}</td>
  </tr>
);

const Powers: Page = () => (
  <Frame eyebrow="角色" title="誰能做什麼，誰不能做什麼">
    <table style={{ width: '100%', borderCollapse: 'collapse' }}>
      <thead>
        <tr>
          <th style={head}>角色</th>
          <th style={head}>能做</th>
          <th style={head}>不能做</th>
        </tr>
      </thead>
      <tbody>
        <Power who="Agent" can="看商品、取報價、提出請購" cannot="碰金鑰、改金額或收款人、自己核准" />
        <Power who="Host" can="組請購單、依 Policy 裁決、記錄、送去付款" cannot="簽名（沒有私鑰）、偽造本人核准" />
        <Power who="Signer" can="驗本人的裝置，在委託範圍內簽付款授權" cannot="簽委託外的付款" />
        <Power who="本人" can="用裝置確認委託；超過上限時核准單筆" cannot="—" />
        <Power who="Checker" can="只讀證據包與鏈，獨立判定十項主張" cannot="呼叫 Host、Policy 或 Signer 的程式" />
      </tbody>
    </table>
  </Frame>
);

// ---------- 06 the five steps ----------
const StepCard = ({ n, title, text }: { n: string; title: string; text: string }) => (
  <div style={{ flex: 1, borderTop: `4px solid ${ink}`, paddingTop: 24 }}>
    <div style={{ fontFamily: 'var(--osd-font-display)', fontSize: 58, fontStyle: 'italic', color: 'var(--osd-accent)', lineHeight: 1 }}>{n}</div>
    <div style={{ fontSize: 42, fontWeight: 800, marginTop: 18 }}>{title}</div>
    <div style={{ fontSize: 28, lineHeight: 1.55, color: muted, marginTop: 16 }}>{text}</div>
  </div>
);

const FiveSteps: Page = () => (
  <Frame eyebrow="流程" title="一筆付款的五步">
    <div style={{ display: 'flex', gap: 30, marginTop: 16 }}>
      <StepCard n="01" title="委託" text="本人用裝置確認：任務預算、自動放行上限、商家、期限。" />
      <StepCard n="02" title="選購" text="Agent 取報價、提出請購；憑證只揭露這間店要求的屬性。" />
      <StepCard n="03" title="核准" text="Policy 裁決。範圍內自動放行，超過交給本人，不符就拒絕。" />
      <StepCard n="04" title="付款" text="兩筆鏈上交易：先預留預算，再結算。" />
      <StepCard n="05" title="交付" text="商店交付並開收據，收據記下內容的雜湊。" />
    </div>
    <div
      style={{
        marginTop: 64,
        background: ink,
        color: '#fbf7ee',
        borderRadius: 'var(--osd-radius)',
        padding: '30px 40px',
        fontSize: 32,
        lineHeight: 1.5,
      }}
    >
      任務、請購、本人授權、預算、付款、交付共用同一個 <b>intent_hash</b>，商家報價的完整內容也算進雜湊。
    </div>
  </Frame>
);

// ---------- 07 the policy ----------
const Chip = ({ label, color, text }: { label: string; color: string; text: string }) => (
  <div style={{ flex: 1, display: 'flex', alignItems: 'baseline', gap: 18 }}>
    <span style={{ fontSize: 30, fontWeight: 800, color: '#fff', background: color, borderRadius: 8, padding: '6px 18px' }}>{label}</span>
    <span style={{ fontSize: 28, color: muted }}>{text}</span>
  </div>
);

const Rule = ({ rule, by }: { rule: string; by: string }) => (
  <tr>
    <td style={{ ...cell, fontWeight: 700, width: 640 }}>{rule}</td>
    <td style={cell}>{by}</td>
  </tr>
);

const PolicyPage: Page = () => (
  <Frame eyebrow="POLICY" title="規則是確定性的，不由模型判斷">
    <div style={{ display: 'flex', gap: 30, marginBottom: 34 }}>
      <Chip label="Allow" color={green} text="在自動範圍內" />
      <Chip label="Ask" color={amber} text="超過上限，交給本人" />
      <Chip label="Deny" color="#d9512c" text="不符規則，付款前擋下" />
    </div>
    <table style={{ width: '100%', borderCollapse: 'collapse' }}>
      <thead>
        <tr>
          <th style={head}>規則</th>
          <th style={head}>由誰執行</th>
        </tr>
      </thead>
      <tbody>
        <Rule rule="這個任務最多花多少" by="委託 ＋ 鏈上的 ZK 預算合約" />
        <Rule rule="單筆超過多少要問本人" by="Policy 裁決，本人用裝置核准" />
        <Rule rule="只能付給名單內的商家" by="Policy ＋ Signer（各擋一次）" />
        <Rule rule="十分鐘內最多幾筆" by="Policy ＋ Signer（各擋一次）" />
        <Rule rule="這間店要求的身分屬性" by="BBS+ 憑證驗證" />
      </tbody>
    </table>
  </Frame>
);

// ---------- 08 the two transactions ----------
const Tx = ({ tag, title, a, b, c }: { tag: string; title: string; a: string; b: string; c: string }) => (
  <div style={{ flex: 1, background: panel, borderRadius: 'var(--osd-radius)', padding: '44px 48px 48px' }}>
    <div style={{ fontSize: 26, letterSpacing: '0.18em', fontWeight: 700, color: 'var(--osd-accent)' }}>{tag}</div>
    <div style={{ fontFamily: 'var(--osd-font-display)', fontSize: 52, fontWeight: 800, marginTop: 14 }}>{title}</div>
    <ul style={{ fontSize: 31, lineHeight: 1.5, margin: '28px 0 0', paddingLeft: 34 }}>
      <li style={{ marginBottom: 12 }}>{a}</li>
      <li style={{ marginBottom: 12 }}>{b}</li>
      <li>{c}</li>
    </ul>
  </div>
);

const TwoTx: Page = () => (
  <Frame eyebrow="鏈上" title="一筆付款是兩筆鏈上交易">
    <div style={{ display: 'flex', gap: 36, marginTop: 12 }}>
      <Tx
        tag="TX #1 · 預留"
        title="ZK 預算預留"
        a="Relayer 送出 Groth16 證明"
        b="合約驗證：扣完不為負、對應這個任務"
        c="鏈上只看得到承諾值，看不到上限"
      />
      <Tx
        tag="TX #2 · 結算"
        title="EIP-3009 結算"
        a="Signer 先確認預算已預留才簽"
        b="授權寫明金額、收款人、有效期限"
        c="Facilitator 送出，商店收到款項"
      />
    </div>
    <p style={{ fontSize: 31, lineHeight: 1.5, marginTop: 44 }}>
      預留與結算分開追蹤。結果不明時先向鏈對帳，不會重付；付了款沒交付，由鏈判斷後退款。
    </p>
  </Frame>
);

// ---------- 09 identity ----------
const Stage = ({ title, text }: { title: string; text: string }) => (
  <div style={{ flex: 1, background: panel, borderRadius: 'var(--osd-radius)', padding: '30px 32px 34px' }}>
    <div style={{ fontSize: 36, fontWeight: 800 }}>{title}</div>
    <div style={{ fontSize: 27, lineHeight: 1.5, color: muted, marginTop: 12 }}>{text}</div>
  </div>
);

const Sees = ({ shop, sees }: { shop: string; sees: string }) => (
  <div style={{ flex: 1, borderTop: `4px solid ${ink}`, paddingTop: 20 }}>
    <div style={{ fontSize: 30, color: muted }}>{shop}</div>
    <div style={{ fontSize: 40, fontWeight: 800, marginTop: 8 }}>{sees}</div>
  </div>
);

const Identity: Page = () => (
  <Frame eyebrow="身分" title="身分驗一次，每間店只看它需要的">
    <div style={{ display: 'flex', alignItems: 'stretch', gap: 18 }}>
      <Stage title="eKYC 廠商" text="證件、活體偵測、人臉比對。姓名、生日、證件號碼留在這裡。" />
      <div style={{ alignSelf: 'center', fontSize: 44, color: 'var(--osd-accent)' }}>→</div>
      <Stage title="發行者" text="只接受廠商自動判定、帶證件資料的通過，算出國家與是否滿 18 歲。" />
      <div style={{ alignSelf: 'center', fontSize: 44, color: 'var(--osd-accent)' }}>→</div>
      <Stage title="BBS+ 憑證" text="出示時選擇性揭露：只給這間店要求的屬性。" />
    </div>
    <div style={{ fontSize: 26, letterSpacing: '0.14em', color: muted, fontWeight: 700, marginTop: 52 }}>每間店看到的</div>
    <div style={{ display: 'flex', gap: 36, marginTop: 18 }}>
      <Sees shop="書店（地區授權）" sees="國家" />
      <Sees shop="咖啡店" sees="什麼都沒有" />
      <Sees shop="便利商店（賣酒）" sees="是否滿 18 歲" />
    </div>
    <p style={{ fontSize: 28, color: muted, marginTop: 44, lineHeight: 1.5 }}>
      三層對照：KYH 確認真人＝eKYC；KYA 確認 Agent 與授權＝委託憑證與 ERC-8004；KYT 交易監控＝還沒做。
    </p>
  </Frame>
);

// ---------- 10 threats ----------
const Threat = ({ threat, stop }: { threat: string; stop: string }) => (
  <tr>
    <td style={{ ...cell, fontWeight: 700, width: 560 }}>{threat}</td>
    <td style={cell}>{stop}</td>
  </tr>
);

const Threats: Page = () => (
  <Frame eyebrow="威脅" title="出事的時候，怎麼擋">
    <table style={{ width: '100%', borderCollapse: 'collapse' }}>
      <thead>
        <tr>
          <th style={head}>情況</th>
          <th style={head}>擋法</th>
        </tr>
      </thead>
      <tbody>
        <Threat threat="Agent 被頁面裡的文字誘導" stop="它只能提出；金額與收款人來自報價，超出預算就拒絕" />
        <Threat threat="付款頁的收款人被換掉" stop="不在商家名單，付款前就拒絕，錢沒有動" />
        <Threat threat="付了款，商店沒交付" stop="由鏈判斷已付款，不重付；商店退回全額後才記退款" />
        <Threat threat="Host 被入侵" stop="Host 沒有私鑰；Signer 自己驗裝置與委託上限" />
        <Threat threat="重放舊的核准或付款" stop="一次性 challenge、EIP-3009 nonce、ZK nullifier" />
      </tbody>
    </table>
    <p style={{ fontSize: 28, color: muted, marginTop: 30, lineHeight: 1.5 }}>
      擋不住的也要說：在自動範圍內買了不該買的東西仍會成交，靠上限與任務預算限制損失。
    </p>
  </Frame>
);

// ---------- 11 the checker ----------
const Claim = ({ id, text }: { id: string; text: string }) => (
  <div style={{ display: 'flex', gap: 22, alignItems: 'baseline', borderTop: `2px solid ${line}`, padding: '17px 0' }}>
    <span style={{ fontFamily: 'var(--osd-font-display)', fontSize: 32, fontStyle: 'italic', color: 'var(--osd-accent)', width: 64 }}>{id}</span>
    <span style={{ fontSize: 29, lineHeight: 1.4 }}>{text}</span>
  </div>
);

const Checker: Page = () => (
  <Frame eyebrow="查證" title="查帳的人不必相信這個系統">
    <p style={{ fontSize: 32, lineHeight: 1.5, margin: '0 0 26px' }}>
      Checker 只讀證據包與鏈，不呼叫系統裡的任何程式。測試網的實跑十項全部通過。
    </p>
    <div style={{ display: 'flex', gap: 70 }}>
      <div style={{ flex: 1 }}>
        <Claim id="E1" text="證據檔案完整、沒有被改動" />
        <Claim id="E2" text="每間店看到的正好是它要求的屬性" />
        <Claim id="E3" text="事件紀錄前後相連、順序正確" />
        <Claim id="E4" text="兩筆交易與 ZK 預算證明（向鏈查證）" />
        <Claim id="E5" text="結果未知時沒有重複付款" />
      </div>
      <div style={{ flex: 1 }}>
        <Claim id="E6" text="每筆紀錄如實標示模擬或真實" />
        <Claim id="E7" text="兩位客人的身分與錢包互不混用" />
        <Claim id="E8" text="期間沒有未經授權的付款（向鏈重算）" />
        <Claim id="E9" text="證據只含允許的欄位，祕密掃描通過" />
        <Claim id="E10" text="誘導文字沒有讓付款越權" />
      </div>
    </div>
  </Frame>
);

// ---------- 12 section ----------
const Section: Page = () => (
  <div style={{ ...fill, background: ink, color: '#fbf7ee', padding: `0 ${PAD + 30}px`, display: 'flex', flexDirection: 'column', justifyContent: 'center' }}>
    <div style={{ fontSize: 26, color: '#f0a184', letterSpacing: '0.24em', fontWeight: 700 }}>第二部分</div>
    <h2 style={{ fontFamily: 'var(--osd-font-display)', fontSize: 110, fontWeight: 900, lineHeight: 1.2, margin: '30px 0 0' }}>
      部署到實際場景
    </h2>
    <p style={{ fontSize: 38, color: '#d8cfbf', margin: '40px 0 0', lineHeight: 1.5 }}>
      現在的展示跑在測試網與沙盒上。要落地，哪些要換、哪些還沒做。
    </p>
  </div>
);

// ---------- 13 the production deployment, with what the PoC does instead ----------
const blue = '#2b6cb0';

const Zone = ({ x, y, w, h, label }: { x: number; y: number; w: number; h: number; label: string }) => (
  <div
    style={{
      position: 'absolute',
      left: x,
      top: y,
      width: w,
      height: h,
      boxSizing: 'border-box',
      border: `2px dashed ${line}`,
      borderRadius: 18,
    }}
  >
    <div style={{ position: 'absolute', left: 16, top: 6, fontSize: 21, letterSpacing: '0.14em', fontWeight: 700, color: muted }}>{label}</div>
  </div>
);

const DNode = ({
  x, y, w, h, title, sub, extra, poc, dark,
}: { x: number; y: number; w: number; h: number; title: string; sub: string; extra?: string; poc?: string; dark?: boolean }) => (
  <div
    style={{
      position: 'absolute',
      left: x,
      top: y,
      width: w,
      height: h,
      boxSizing: 'border-box',
      background: dark ? ink : '#fffdf8',
      color: dark ? '#fbf7ee' : ink,
      border: `2px solid ${dark ? ink : '#c9bfab'}`,
      borderRadius: 12,
      padding: '0 16px',
      display: 'flex',
      flexDirection: 'column',
      justifyContent: 'center',
    }}
  >
    <div style={{ fontSize: 27, fontWeight: 800, lineHeight: 1.25 }}>{title}</div>
    <div style={{ fontSize: 22, lineHeight: 1.3, color: dark ? '#d8cfbf' : muted }}>{sub}</div>
    {extra ? <div style={{ fontSize: 22, lineHeight: 1.3, color: dark ? '#d8cfbf' : muted }}>{extra}</div> : null}
    {poc ? <div style={{ fontSize: 21, lineHeight: 1.35, color: dark ? '#f0b869' : amber, fontWeight: 600 }}>PoC：{poc}</div> : null}
  </div>
);

const Flow = ({
  d, kind, label, lx, ly, both,
}: { d: string; kind: 'cash' | 'auth' | 'id' | 'proof'; label?: string; lx?: number; ly?: number; both?: boolean }) => {
  const color = kind === 'cash' ? '#d9512c' : kind === 'auth' ? ink : kind === 'id' ? blue : muted;
  return (
    <g>
      <path
        d={d}
        fill="none"
        stroke={color}
        strokeWidth={kind === 'cash' ? 9 : 3}
        strokeDasharray={kind === 'id' ? '3 7' : kind === 'proof' ? '12 8' : undefined}
        strokeLinecap={kind === 'id' ? 'round' : undefined}
        markerEnd={`url(#m-${kind})`}
        markerStart={both ? `url(#m-${kind})` : undefined}
      />
      {label ? (
        <text x={lx} y={ly} fontSize={kind === 'cash' ? 26 : 21} fontWeight={kind === 'cash' ? 800 : 500} fill={color} fontFamily="-apple-system, PingFang TC, sans-serif">
          {label}
        </text>
      ) : null}
    </g>
  );
};

const Marker = ({ id, color, size }: { id: string; color: string; size: number }) => (
  <marker id={id} viewBox="0 0 10 10" refX="8.5" refY="5" markerWidth={size} markerHeight={size} orient="auto-start-reverse">
    <path d="M0 0 L10 5 L0 10 z" fill={color} />
  </marker>
);

const Key = ({ x, y, kind, title, text }: { x: number; y: number; kind: 'cash' | 'auth' | 'id' | 'proof'; title: string; text: string }) => (
  <div style={{ position: 'absolute', left: x, top: y, display: 'flex', alignItems: 'center', gap: 14 }}>
    <svg width={64} height={20} aria-hidden="true">
      <Flow d="M2 10 L54 10" kind={kind} />
    </svg>
    <span style={{ fontSize: 23, fontWeight: 800, color: kind === 'cash' ? '#d9512c' : kind === 'auth' ? ink : kind === 'id' ? blue : muted }}>{title}</span>
    <span style={{ fontSize: 22, color: muted }}>{text}</span>
  </div>
);

const Deploy: Page = () => (
  <div style={{ ...fill, padding: '58px 80px 0' }}>
    <div style={{ fontSize: 24, color: 'var(--osd-accent)', letterSpacing: '0.22em', fontWeight: 700 }}>部署架構</div>
    <h2 style={{ fontFamily: 'var(--osd-font-display)', fontSize: 54, fontWeight: 800, lineHeight: 1.2, margin: '10px 0 0' }}>
      正式環境：錢、授權、身分、證據，各走各的路
    </h2>
    <div style={{ position: 'relative', width: 1760, height: 800, marginTop: 26 }}>
      <Zone x={0} y={0} w={1000} h={166} label="身分域" />
      <Zone x={0} y={190} w={350} h={440} label="使用者端" />
      <Zone x={390} y={190} w={420} h={250} label="授權服務" />
      <Zone x={390} y={466} w={420} h={164} label="金鑰域（隔離）" />
      <Zone x={850} y={190} w={400} h={440} label="付款網路" />
      <Zone x={1290} y={0} w={470} h={630} label="鏈" />
      <Zone x={850} y={646} w={400} h={154} label="稽核" />

      <div style={{ position: 'absolute', left: 1320, top: 196, width: 410, height: 410, boxSizing: 'border-box', border: `2px solid #c9bfab`, borderRadius: 12, background: '#f6efe0' }}>
        <div style={{ padding: '10px 16px 0', fontSize: 25, fontWeight: 800 }}>穩定幣合約（EIP-3009）</div>
        <div style={{ padding: '0 16px', fontSize: 21, color: amber, fontWeight: 600 }}>PoC：測試 USDC</div>
      </div>

      <svg width={1760} height={800} style={{ position: 'absolute', left: 0, top: 0 }} aria-hidden="true">
        <defs>
          <Marker id="m-cash" color="#d9512c" size={4} />
          <Marker id="m-auth" color={ink} size={8} />
          <Marker id="m-id" color={blue} size={8} />
          <Marker id="m-proof" color={muted} size={8} />
        </defs>
        <Flow kind="id" d="M30 530 L13 530 L13 96 L30 96" label="證件・自拍" lx={30} ly={184} />
        <Flow kind="id" d="M320 96 L360 96" />
        <Flow kind="id" d="M545 156 L545 228" label="憑證" lx={557} ly={184} />
        <Flow kind="id" d="M740 156 L740 228" label="撤銷狀態" lx={752} ly={184} />
        <Flow kind="auth" d="M320 292 L420 292" label="請購" lx={348} ly={282} />
        <Flow kind="auth" d="M320 560 L420 560" label="核准" lx={348} ly={550} />
        <Flow kind="auth" d="M600 418 L600 500" both label="簽名請求" lx={612} ly={462} />
        <Flow kind="auth" d="M780 262 L880 262" label="證明" lx={808} ly={252} />
        <Flow kind="auth" d="M780 340 L880 400" label="授權" lx={790} ly={398} />
        <Flow kind="auth" d="M1050 480 L1050 506" />
        <Flow kind="auth" d="M1220 262 L1320 130" label="tx#1" lx={1268} ly={232} />
        <Flow kind="auth" d="M1220 560 L1320 440" label="tx#2" lx={1262} ly={540} />
        <Flow kind="cash" d="M1525 360 L1525 470" label="金流" lx={1548} ly={426} />
        <Flow kind="proof" d="M760 418 L760 453 L830 453 L830 738 L880 738" label="證據包" lx={752} ly={700} />
        <Flow kind="proof" d="M1525 606 L1525 738 L1220 738" label="鏈上紀錄" lx={1300} ly={726} />
      </svg>

      <DNode x={30} y={36} w={290} h={120} title="eKYC 廠商" sub="證件、活體、人臉比對" poc="Didit 沙盒" />
      <DNode x={360} y={36} w={290} h={120} title="發行者" sub="簽發憑證，獨立部署" poc="與 Host 同程序" />
      <DNode x={690} y={36} w={280} h={120} title="狀態清單" sub="撤銷與過期" poc="Host 定時重簽" />

      <DNode x={30} y={232} w={290} h={120} title="Agent 執行環境" sub="不受信任，只能提出" poc="本機 CLI" />
      <DNode x={30} y={470} w={290} h={120} title="本人的手機" sub="passkey 確認與核准" poc="同一台筆電" />

      <DNode x={420} y={228} w={360} h={190} title="Host（授權服務）" sub="Policy・Store・憑證驗證" extra="不持有任何私鑰" poc="單機、SQLite" />
      <DNode x={420} y={500} w={360} h={120} dark title="Signer ＋ HSM／MPC" sub="只簽委託範圍內" poc="系統帳號的金鑰檔" />

      <DNode x={880} y={222} w={340} h={118} title="Relayer" sub="送 ZK 預留、付 gas" poc="本機、測試金鑰" />
      <DNode x={880} y={362} w={340} h={118} title="商店（x402 端點）" sub="報價、交付、收據" poc="主持人代管" />
      <DNode x={880} y={506} w={340} h={118} title="Facilitator" sub="驗證並送出結算" poc="自架，與 Host 同機" />

      <DNode x={1320} y={36} w={410} h={120} title="預算合約" sub="Groth16 驗證、預留預算" poc="測試網、單方 setup" />
      <DNode x={1345} y={270} w={360} h={90} title="客人的錢包" sub="餘額在鏈上，私鑰在 Signer" />
      <DNode x={1345} y={476} w={360} h={90} title="商店的錢包" sub="收款；退款時反向" />

      <DNode x={880} y={680} w={340} h={112} title="Checker｜稽核方" sub="只讀證據包與鏈" poc="同一支獨立程式" />

      <Key x={0} y={646} kind="cash" title="金流" text="只在鏈上，不經過 Agent 與 Host" />
      <Key x={0} y={682} kind="auth" title="授權" text="誰提出、誰裁決、誰簽名" />
      <Key x={0} y={718} kind="id" title="身分資料" text="個資只到 eKYC 廠商" />
      <Key x={0} y={754} kind="proof" title="證據" text="事後由第三方重算" />
    </div>
    <Footer />
  </div>
);

// ---------- 14 one transaction, a step each "→": the same deployment, the 0.42 birthday cake ----------
// The framework's <Step> carries data-osd-step ("pending" / "revealed"): the newest revealed step is the active one
// (its lines flow, a dot runs along each, its nodes pulse, its caption shows); the ones before it settle to a
// faint trace. Jumped to from the overview, every step is revealed: the whole path traced, the last caption.
const TX_CSS = `
.apa-tx-line { fill: none; stroke-width: 6; stroke-linecap: round; stroke-dasharray: 16 12; }
.apa-tx-dot { offset-rotate: 0deg; offset-distance: 0%; opacity: 0; }
.apa-tx-ring { position: absolute; box-sizing: border-box; border: 4px solid #d9512c; border-radius: 14px; pointer-events: none; }
.apa-tx-coin { position: absolute; left: 1491px; top: 322px; width: 68px; height: 68px; border-radius: 50%; background: #d9512c; color: #fff;
  font-size: 22px; font-weight: 800; display: flex; align-items: center; justify-content: center; box-shadow: 0 6px 18px rgba(217,81,44,.45); opacity: 0; }
[data-osd-step="revealed"] .apa-tx-line { animation: apa-tx-dash .8s linear infinite; }
[data-osd-step="revealed"] .apa-tx-dot { animation: apa-tx-move 1.9s cubic-bezier(.4,0,.2,1) infinite; }
[data-osd-step="revealed"] .apa-tx-ring { animation: apa-tx-pulse 1.4s ease-out infinite; }
[data-osd-step="revealed"] .apa-tx-coin { animation: apa-tx-coin 2.2s cubic-bezier(.45,0,.2,1) infinite; }
[data-osd-step="revealed"] .apa-tx-coin-back { left: 1531px; animation-name: apa-tx-coin-back; }
[data-osd-step="revealed"]:has(~ [data-osd-step="revealed"]) .apa-tx-line { animation: none; stroke-dasharray: none; stroke-width: 4; opacity: .32; }
[data-osd-step="revealed"]:has(~ [data-osd-step="revealed"]) :is(.apa-tx-dot, .apa-tx-ring, .apa-tx-coin, .apa-tx-caption, .apa-tx-stop, .apa-tx-stamp, .apa-tx-tag) { display: none; }
.apa-tx-root:has([data-osd-step="revealed"]) .apa-tx-intro { display: none; }
.apa-tx-stop { position: absolute; width: 46px; height: 46px; margin: -23px 0 0 -23px; border-radius: 50%; background: #b3261e; color: #fff; font-size: 28px; font-weight: 900;
  display: flex; align-items: center; justify-content: center; border: 3px solid #fffdf8; box-shadow: 0 4px 12px rgba(179,38,30,.4); }
.apa-tx-stamp { position: absolute; box-sizing: border-box; border: 5px solid #b3261e; color: #b3261e; border-radius: 10px; padding: 6px 20px; font-size: 34px; font-weight: 900;
  letter-spacing: .12em; transform: rotate(-8deg); background: rgba(255,253,248,.9); }
.apa-tx-tag { position: absolute; background: #d9512c; color: #fff; border-radius: 999px; padding: 4px 16px; font-size: 22px; font-weight: 800; white-space: nowrap; box-shadow: 0 4px 12px rgba(217,81,44,.35); }
[data-osd-step="revealed"] :is(.apa-tx-stop, .apa-tx-tag) { animation: apa-tx-pop .35s cubic-bezier(.2,1.4,.4,1) both; }
[data-osd-step="revealed"] .apa-tx-stamp { animation: apa-tx-slam .4s cubic-bezier(.2,1.3,.4,1) both; }
@keyframes apa-tx-pop { from { transform: scale(.2); opacity: 0; } to { transform: scale(1); opacity: 1; } }
@keyframes apa-tx-slam { from { transform: rotate(-8deg) scale(1.9); opacity: 0; } to { transform: rotate(-8deg) scale(1); opacity: 1; } }
@keyframes apa-tx-dash { from { stroke-dashoffset: 28; } to { stroke-dashoffset: 0; } }
@keyframes apa-tx-move { 0% { offset-distance: 0%; opacity: 0; } 12% { opacity: 1; } 88% { opacity: 1; } 100% { offset-distance: 100%; opacity: 0; } }
@keyframes apa-tx-pulse { 0% { box-shadow: 0 0 0 0 rgba(217,81,44,.5); } 100% { box-shadow: 0 0 0 18px rgba(217,81,44,0); } }
@keyframes apa-tx-coin { 0% { top: 322px; opacity: 0; } 15% { opacity: 1; } 80% { opacity: 1; } 100% { top: 462px; opacity: 0; } }
@keyframes apa-tx-coin-back { 0% { top: 462px; opacity: 0; } 15% { opacity: 1; } 80% { opacity: 1; } 100% { top: 322px; opacity: 0; } }
@media (prefers-reduced-motion: reduce) { .apa-tx-line, .apa-tx-dot, .apa-tx-ring, .apa-tx-coin { animation: none !important; } .apa-tx-coin { opacity: 1; top: 392px; } }
`;

const TxPath = ({ d, kind }: { d: string; kind: 'cash' | 'auth' | 'id' | 'proof' }) => {
  const color = kind === 'cash' ? '#d9512c' : kind === 'auth' ? ink : kind === 'id' ? blue : '#5c6470';
  return (
    <g>
      <path className="apa-tx-line" d={d} stroke={color} markerEnd={`url(#t-${kind})`} />
      <circle className="apa-tx-dot" r={11} fill={color} stroke="#fffdf8" strokeWidth={3} style={{ offsetPath: `path("${d}")` }} />
    </g>
  );
};

const Stop = ({ x, y }: { x: number; y: number }) => (
  <div className="apa-tx-stop" style={{ left: x, top: y }}>
    ✕
  </div>
);

const Tag = ({ x, y, text }: { x: number; y: number; text: string }) => (
  <div className="apa-tx-tag" style={{ left: x, top: y }}>
    {text}
  </div>
);

const Ring = ({ x, y, w, h }: { x: number; y: number; w: number; h: number }) => (
  <div className="apa-tx-ring" style={{ left: x - 5, top: y - 5, width: w + 10, height: h + 10 }} />
);

const Caption = ({ n, title, text, intro }: { n: string; title: string; text: string; intro?: boolean }) => (
  <div
    className={intro ? 'apa-tx-intro' : 'apa-tx-caption'}
    style={{ position: 'absolute', left: 0, top: 650, width: 810, height: 150, boxSizing: 'border-box', background: ink, color: '#fbf7ee', borderRadius: 14, padding: '13px 26px' }}
  >
    <div style={{ display: 'flex', alignItems: 'baseline', gap: 16 }}>
      <span style={{ fontFamily: 'var(--osd-font-display)', fontStyle: 'italic', fontSize: 38, color: '#f0a184', lineHeight: 1 }}>{n}</span>
      <span style={{ fontSize: 32, fontWeight: 800 }}>{title}</span>
    </div>
    <div style={{ fontSize: 26, lineHeight: 1.45, marginTop: 8, color: '#e9e1d2' }}>{text}</div>
  </div>
);

const TxSvg = ({ children }: { children: ReactNode }) => (
  <svg width={1760} height={800} style={{ position: 'absolute', left: 0, top: 0, overflow: 'visible' }} aria-hidden="true">
    {children}
  </svg>
);

const Faint = ({ d }: { d: string }) => <path d={d} fill="none" stroke="#d3cab8" strokeWidth={2.5} markerEnd="url(#t-faint)" />;

const TxStage = ({
  eyebrow, title, agent, shop, wallet, intro, children,
}: { eyebrow: string; title: string; agent: string; shop: string; wallet: string; intro: string; children: ReactNode }) => (
  <div style={{ ...fill, padding: '58px 80px 0' }}>
    <style>{TX_CSS}</style>
    <div style={{ fontSize: 24, color: 'var(--osd-accent)', letterSpacing: '0.22em', fontWeight: 700 }}>{eyebrow}</div>
    <h2 style={{ fontFamily: 'var(--osd-font-display)', fontSize: 54, fontWeight: 800, lineHeight: 1.2, margin: '10px 0 0' }}>
      {title}
    </h2>
    <div className="apa-tx-root" style={{ position: 'relative', width: 1760, height: 800, marginTop: 26 }}>
      <Zone x={0} y={0} w={1000} h={166} label="身分域" />
      <Zone x={0} y={190} w={350} h={440} label="使用者端" />
      <Zone x={390} y={190} w={420} h={250} label="授權服務" />
      <Zone x={390} y={466} w={420} h={164} label="金鑰域（隔離）" />
      <Zone x={850} y={190} w={400} h={440} label="付款網路" />
      <Zone x={1290} y={0} w={470} h={630} label="鏈" />
      <Zone x={850} y={646} w={400} h={154} label="稽核" />
      <div style={{ position: 'absolute', left: 1320, top: 196, width: 410, height: 410, boxSizing: 'border-box', border: `2px solid #c9bfab`, borderRadius: 12, background: '#f6efe0' }}>
        <div style={{ padding: '14px 16px 0', fontSize: 25, fontWeight: 800 }}>穩定幣合約（EIP-3009）</div>
      </div>

      <TxSvg>
        <defs>
          <Marker id="t-faint" color="#d3cab8" size={7} />
          <Marker id="t-cash" color="#d9512c" size={4} />
          <Marker id="t-auth" color={ink} size={5} />
          <Marker id="t-id" color={blue} size={5} />
          <Marker id="t-proof" color="#5c6470" size={5} />
        </defs>
        <Faint d="M30 530 L13 530 L13 96 L30 96" />
        <Faint d="M320 96 L360 96" />
        <Faint d="M545 156 L545 228" />
        <Faint d="M740 156 L740 228" />
        <Faint d="M320 292 L420 292" />
        <Faint d="M320 560 L420 560" />
        <Faint d="M600 418 L600 500" />
        <Faint d="M780 262 L880 262" />
        <Faint d="M780 340 L880 400" />
        <Faint d="M1050 480 L1050 506" />
        <Faint d="M1220 262 L1320 130" />
        <Faint d="M1220 560 L1320 440" />
        <Faint d="M1525 360 L1525 470" />
        <Faint d="M760 418 L760 453 L830 453 L830 738 L880 738" />
        <Faint d="M1525 606 L1525 738 L1220 738" />
      </TxSvg>

      <DNode x={30} y={36} w={290} h={120} title="eKYC 廠商" sub="證件、活體、人臉比對" />
      <DNode x={360} y={36} w={290} h={120} title="發行者" sub="簽發憑證" />
      <DNode x={690} y={36} w={280} h={120} title="狀態清單" sub="撤銷與過期" />
      <DNode x={30} y={232} w={290} h={120} title={agent} sub="不受信任，只能提出" />
      <DNode x={30} y={470} w={290} h={120} title="家長的手機" sub="passkey 確認與核准" />
      <DNode x={420} y={228} w={360} h={190} title="Host（授權服務）" sub="Policy・Store・憑證驗證" extra="不持有任何私鑰" />
      <DNode x={420} y={500} w={360} h={120} dark title="Signer" sub="只簽委託範圍內" />
      <DNode x={880} y={222} w={340} h={118} title="Relayer" sub="送 ZK 預留、付 gas" />
      <DNode x={880} y={362} w={340} h={118} title={shop} sub="報價、交付、收據" />
      <DNode x={880} y={506} w={340} h={118} title="Facilitator" sub="驗證並送出結算" />
      <DNode x={1320} y={36} w={410} h={120} title="預算合約" sub="Groth16 驗證、預留預算" />
      <DNode x={1345} y={270} w={360} h={90} title="客人的錢包" sub="餘額在鏈上，私鑰在 Signer" />
      <DNode x={1345} y={476} w={360} h={90} title={wallet} sub="收款；退款時反向" />
      <DNode x={880} y={680} w={340} h={112} title="Checker｜稽核方" sub="只讀證據包與鏈" />

      <Caption intro n="00" title="開始" text={intro} />

      <Steps>
        {children}
      </Steps>
    </div>
    <Footer />
  </div>
);


const Walkthrough: Page = () => (
  <TxStage
    eyebrow="交易實況 1"
    title="順利的一筆：0.42 USDC 的生日蛋糕"
    agent="孩子的 Agent"
    shop="咖啡店（x402 端點）"
    wallet="咖啡店的錢包"
    intro="孩子的 AI 要訂一個 0.42 USDC 的生日蛋糕。按 → 一步一步走完這筆交易。"
  >
    <Step>
      <TxSvg>
        <TxPath kind="id" d="M30 530 L13 530 L13 96 L30 96" />
        <TxPath kind="id" d="M320 96 L360 96" />
        <TxPath kind="id" d="M545 156 L545 228" />
      </TxSvg>
      <Ring x={30} y={36} w={290} h={120} />
      <Ring x={360} y={36} w={290} h={120} />
      <Caption n="01" title="身分（事前）" text="家長先通過 eKYC，發行者才簽發憑證。證件與自拍留在 eKYC 廠商。" />
    </Step>
    <Step>
      <TxSvg>
        <TxPath kind="auth" d="M320 560 L420 560" />
      </TxSvg>
      <Ring x={30} y={470} w={290} h={120} />
      <Ring x={420} y={500} w={360} h={120} />
      <Caption n="02" title="委託" text="家長在手機上確認委託：這次最多花 1.50，單筆超過 0.10 要問我。" />
    </Step>
    <Step>
      <TxSvg>
        <TxPath kind="auth" d="M320 292 L420 292" />
        <TxPath kind="auth" d="M780 340 L880 400" />
      </TxSvg>
      <Ring x={30} y={232} w={290} h={120} />
      <Ring x={880} y={362} w={340} h={118} />
      <Caption n="03" title="選購" text="Agent 向咖啡店取得報價 0.42，提出請購。這間店不要求任何個資。" />
    </Step>
    <Step>
      <TxSvg>
        <TxPath kind="auth" d="M320 580 L420 580" />
      </TxSvg>
      <Ring x={420} y={228} w={360} h={190} />
      <Ring x={30} y={470} w={290} h={120} />
      <Caption n="04" title="核准" text="0.42 超過 0.10，Policy 判定 Ask。家長在手機上核准這一筆。" />
    </Step>
    <Step>
      <TxSvg>
        <TxPath kind="auth" d="M780 262 L880 262" />
        <TxPath kind="auth" d="M1220 262 L1320 130" />
      </TxSvg>
      <Ring x={880} y={222} w={340} h={118} />
      <Ring x={1320} y={36} w={410} h={120} />
      <Caption n="05" title="預留・tx#1" text="Relayer 送出 ZK 證明；預算合約確認扣掉 0.42 後不為負，預留預算。" />
    </Step>
    <Step>
      <TxSvg>
        <TxPath kind="auth" d="M600 418 L600 500" />
        <TxPath kind="auth" d="M640 500 L640 418" />
      </TxSvg>
      <Ring x={420} y={500} w={360} h={120} />
      <Caption n="06" title="簽名" text="Signer 確認預算已預留、商家與金額都在委託內，才簽付款授權。" />
    </Step>
    <Step>
      <TxSvg>
        <TxPath kind="auth" d="M780 360 L880 420" />
        <TxPath kind="auth" d="M1050 480 L1050 506" />
        <TxPath kind="auth" d="M1220 560 L1320 440" />
        <TxPath kind="cash" d="M1525 360 L1525 470" />
      </TxSvg>
      <div className="apa-tx-coin">0.42</div>
      <Ring x={880} y={506} w={340} h={118} />
      <Ring x={1345} y={270} w={360} h={90} />
      <Ring x={1345} y={476} w={360} h={90} />
      <Caption n="07" title="結算・tx#2" text="Facilitator 把授權送上鏈：0.42 USDC 從客人的錢包到咖啡店的錢包。" />
    </Step>
    <Step>
      <TxSvg>
        <TxPath kind="auth" d="M880 440 L780 380" />
      </TxSvg>
      <Ring x={880} y={362} w={340} h={118} />
      <Caption n="08" title="交付" text="咖啡店交付訂單並開收據；收據記下交付內容的雜湊。" />
    </Step>
    <Step>
      <TxSvg>
        <TxPath kind="proof" d="M760 418 L760 453 L830 453 L830 738 L880 738" />
        <TxPath kind="proof" d="M1525 606 L1525 738 L1220 738" />
      </TxSvg>
      <Ring x={880} y={680} w={340} h={112} />
      <Caption n="09" title="稽核" text="Checker 只讀證據包與鏈上紀錄，重算這一筆從委託到交付的每一步。" />
    </Step>
  </TxStage>
);

// The second case (docs/commercial-refund.md, the post office): allowed without the person, settled, not delivered;
// the chain says it was paid, a person decides to refund, the shop's key sends the money back, the Host confirms on chain.
const VERIFY = 'M1320 590 L1270 590 L1270 638 L830 638 L830 453 L760 453 L760 418'; // the Host reads the chain itself

const RefundCase: Page = () => (
  <TxStage
    eyebrow="交易實況 2"
    title="出事的一筆：付了 0.08 USDC，郵局沒交付"
    agent="家長的 Agent"
    shop="郵局（x402 端點）"
    wallet="郵局的錢包"
    intro="家長的 AI 要付 0.08 USDC 的包裹郵資。這個情境目前只在本機測試鏈跑過。"
  >
    <Step>
      <TxSvg>
        <TxPath kind="auth" d="M320 292 L420 292" />
        <TxPath kind="auth" d="M780 340 L880 400" />
      </TxSvg>
      <Ring x={30} y={232} w={290} h={120} />
      <Ring x={420} y={228} w={360} h={190} />
      <Caption n="01" title="選購與裁決" text="郵局只看國家。0.08 在 0.10 以內，Policy 自動放行，不用問本人。" />
    </Step>
    <Step>
      <TxSvg>
        <TxPath kind="auth" d="M780 262 L880 262" />
        <TxPath kind="auth" d="M1220 262 L1320 130" />
        <TxPath kind="auth" d="M600 418 L600 500" />
        <TxPath kind="auth" d="M640 500 L640 418" />
      </TxSvg>
      <Ring x={880} y={222} w={340} h={118} />
      <Ring x={420} y={500} w={360} h={120} />
      <Caption n="02" title="預留與簽名" text="沒有人介入：Relayer 預留預算（tx#1），Signer 在委託範圍內簽出授權。" />
    </Step>
    <Step>
      <TxSvg>
        <TxPath kind="auth" d="M780 360 L880 420" />
        <TxPath kind="auth" d="M1050 480 L1050 506" />
        <TxPath kind="auth" d="M1220 560 L1320 440" />
        <TxPath kind="cash" d="M1525 360 L1525 470" />
      </TxSvg>
      <div className="apa-tx-coin">0.08</div>
      <Ring x={880} y={506} w={340} h={118} />
      <Ring x={1345} y={476} w={360} h={90} />
      <Caption n="03" title="結算・tx#2" text="Facilitator 把授權送上鏈：0.08 USDC 到了郵局的錢包。" />
    </Step>
    <Step>
      <TxSvg>
        <TxPath kind="proof" d={VERIFY} />
      </TxSvg>
      <Ring x={880} y={362} w={340} h={118} />
      <Ring x={420} y={228} w={360} h={190} />
      <Caption n="04" title="沒有交付" text="郵局沒交出寄件單。Host 不信商店的說法，向鏈查證：已付款，記交付失敗，不重付。" />
    </Step>
    <Step>
      <TxSvg>
        <TxPath kind="auth" d="M780 380 L880 440" />
        <TxPath kind="auth" d="M1090 480 L1090 506" />
      </TxSvg>
      <Ring x={420} y={228} w={360} h={190} />
      <Ring x={880} y={362} w={340} h={118} />
      <Caption n="05" title="人來判斷" text="該不該退由人判斷。營運人員按下「商店退款」，Host 請郵局退回這一筆。" />
    </Step>
    <Step>
      <TxSvg>
        <TxPath kind="auth" d="M1220 580 L1320 460" />
        <TxPath kind="cash" d="M1565 476 L1565 366" />
      </TxSvg>
      <div className="apa-tx-coin apa-tx-coin-back">0.08</div>
      <Ring x={880} y={506} w={340} h={118} />
      <Ring x={1345} y={270} w={360} h={90} />
      <Caption n="06" title="退款・tx#3" text="郵局的金鑰送出第 3 筆交易：0.08 USDC 全額退回客人的錢包。" />
    </Step>
    <Step>
      <TxSvg>
        <TxPath kind="proof" d={VERIFY} />
        <TxPath kind="proof" d="M760 418 L760 453 L830 453 L830 738 L880 738" />
        <TxPath kind="proof" d="M1525 606 L1525 738 L1220 738" />
      </TxSvg>
      <Ring x={420} y={228} w={360} h={190} />
      <Ring x={880} y={680} w={340} h={112} />
      <Caption n="07" title="確認與稽核" text="Host 向鏈確認是全額、退回付款的錢包，才記退款。Checker 也重算這一筆。" />
    </Step>
  </TxStage>
);

// The third case (docs/commercial-venues.md, the mart's beer): the child's Agent is refused before any payment, the
// parent's is allowed for the same goods. What the two technologies are for: eKYC makes "18 or over" more than the
// buyer's word; the zero-knowledge proofs keep the birth date from the shop and the budget from the chain.
const DeniedCase: Page = () => (
  <TxStage
    eyebrow="交易實況 3"
    title="被擋下的一筆：孩子的 AI 買 0.09 USDC 的啤酒"
    agent="兩位客人的 Agent"
    shop="便利商店（x402 端點）"
    wallet="便利商店的錢包"
    intro="同一件商品，孩子的 AI 買不到，家長的 AI 買得到。差別在身分，不在 AI 說了什麼。"
  >
    <Step>
      <TxSvg>
        <TxPath kind="id" d="M30 530 L13 530 L13 96 L30 96" />
        <TxPath kind="id" d="M320 96 L360 96" />
        <TxPath kind="id" d="M545 156 L545 228" />
      </TxSvg>
      <Ring x={30} y={36} w={290} h={120} />
      <Ring x={360} y={36} w={290} h={120} />
      <Tag x={150} y={20} text="eKYC" />
      <Caption n="01" title="身分（事前）" text="家長的生日經 eKYC 驗證，憑證只寫「已滿 18 歲」。孩子由家長申請，寫的是「未滿 18 歲」。" />
    </Step>
    <Step>
      <TxSvg>
        <TxPath kind="auth" d="M320 292 L420 292" />
        <TxPath kind="auth" d="M780 340 L880 400" />
      </TxSvg>
      <Ring x={30} y={232} w={290} h={120} />
      <Ring x={880} y={362} w={340} h={118} />
      <Caption n="02" title="孩子的請購" text="孩子的 AI 要買啤酒。便利商店只要求一件事：滿 18 歲了嗎？" />
    </Step>
    <Step>
      <TxSvg>
        <TxPath kind="id" d="M740 156 L740 228" />
        <TxPath kind="id" d="M780 380 L880 440" />
      </TxSvg>
      <Ring x={420} y={228} w={360} h={190} />
      <Tag x={470} y={212} text="BBS+ 零知識出示" />
      <Caption n="03" title="出示憑證" text="憑證用 BBS+ 出示：只揭露「是否滿 18 歲」這一項。生日、姓名、證件號碼都不在出示的內容裡。" />
    </Step>
    <Step>
      <Ring x={420} y={228} w={360} h={190} />
      <Stop x={830} y={262} />
      <Stop x={620} y={459} />
      <Stop x={1270} y={500} />
      <div className="apa-tx-stamp" style={{ left: 1372, top: 386 }}>
        錢沒有動
      </div>
      <Caption n="04" title="拒絕" text="屬性不符，Policy 判定 Deny。沒有預留、沒有簽名、沒有結算，付款前就擋下。" />
    </Step>
    <Step>
      <TxSvg>
        <TxPath kind="auth" d="M780 262 L880 262" />
        <TxPath kind="auth" d="M1220 262 L1320 130" />
        <TxPath kind="auth" d="M600 418 L600 500" />
        <TxPath kind="auth" d="M1050 480 L1050 506" />
        <TxPath kind="auth" d="M1220 560 L1320 440" />
        <TxPath kind="cash" d="M1525 360 L1525 470" />
      </TxSvg>
      <div className="apa-tx-coin">0.09</div>
      <Ring x={1320} y={36} w={410} h={120} />
      <Tag x={1470} y={20} text="Groth16 零知識預算" />
      <Caption n="05" title="對照：家長買同一件" text="家長的憑證是「已滿 18 歲」，放行。ZK 證明「扣完不為負」，鏈上看不到預算上限與餘額。" />
    </Step>
    <Step>
      <TxSvg>
        <TxPath kind="proof" d="M760 418 L760 453 L830 453 L830 738 L880 738" />
        <TxPath kind="proof" d="M1525 606 L1525 738 L1220 738" />
      </TxSvg>
      <Ring x={880} y={680} w={340} h={112} />
      <Caption n="06" title="稽核" text="Checker 驗證每間店看到的正好是它要求的屬性，不多不少；被拒絕的那一筆沒有任何鏈上交易。" />
    </Step>
    <Step>
      <Ring x={30} y={36} w={290} h={120} />
      <Ring x={420} y={228} w={360} h={190} />
      <Ring x={1320} y={36} w={410} h={120} />
      <Tag x={150} y={20} text="eKYC" />
      <Tag x={470} y={212} text="BBS+ 零知識出示" />
      <Tag x={1470} y={20} text="Groth16 零知識預算" />
      <Caption n="07" title="少了這兩項技術" text="沒有 eKYC，「滿 18 歲」只是自己說的。沒有零知識證明，店家會看到生日，鏈上會看到預算。" />
    </Step>
  </TxStage>
);

// ---------- 17 what to replace ----------
const Swap = ({ part, now, real }: { part: string; now: string; real: string }) => (
  <tr>
    <td style={{ ...cell, fontWeight: 800, width: 250, padding: '15px 24px 15px 0' }}>{part}</td>
    <td style={{ ...cell, color: muted, width: 600, padding: '15px 24px 15px 0' }}>{now}</td>
    <td style={{ ...cell, padding: '15px 0 15px 0' }}>{real}</td>
  </tr>
);

const Replace: Page = () => (
  <Frame eyebrow="落地" title="元件對照：現在是什麼，要換成什麼">
    <table style={{ width: '100%', borderCollapse: 'collapse' }}>
      <thead>
        <tr>
          <th style={head}>元件</th>
          <th style={head}>現在的展示</th>
          <th style={head}>實際場景（建議）</th>
        </tr>
      </thead>
      <tbody>
        <Swap part="鏈與資產" now="Base Sepolia、測試 USDC" real="正式鏈與正式穩定幣，換設定與合約地址" />
        <Swap part="Signer 的金鑰" now="另一個系統帳號裡的金鑰檔" real="HSM 或 MPC，放在獨立的主機" />
        <Swap part="身分來源" now="Didit 沙盒，沒有驗證真人" real="eKYC 正式環境；發行者獨立部署" />
        <Swap part="本人核准" now="同一台筆電、同步的 passkey" real="本人的手機；大額改用綁裝置、不同步的金鑰" />
        <Swap part="結算" now="自架 facilitator，與 Host 同機" real="自營或第三方的 x402 facilitator" />
        <Swap part="商店" now="固定名單，由主持人代管" real="商店自己的 x402 端點與接入流程" />
      </tbody>
    </table>
    <p style={{ fontSize: 27, color: muted, marginTop: 24, lineHeight: 1.5 }}>
      Agent、Policy、本人核准、ZK 預算與 Signer 的介面不必改；換的是它們背後的環境。
    </p>
  </Frame>
);

// ---------- 18 gaps ----------
const Gap = ({ title, text }: { title: string; text: string }) => (
  <div style={{ display: 'flex', gap: 28, alignItems: 'baseline', borderTop: `2px solid ${line}`, padding: '13px 0' }}>
    <span style={{ fontSize: 31, fontWeight: 800, width: 520, flexShrink: 0 }}>{title}</span>
    <span style={{ fontSize: 29, lineHeight: 1.45, color: muted }}>{text}</span>
  </div>
);

const Gaps: Page = () => (
  <Frame eyebrow="落地" title="部署前還沒做到的事">
    <Gap title="沒有在正式鏈上跑過" text="只有測試網與測試幣；飯店、郵局、退款只在本機鏈跑過" />
    <Gap title="政策在鏈下執行" text="靠 Signer 只簽合規的授權；合約與錢包層不強制" />
    <Gap title="ZK 的 setup 是單方產生的" text="信任它等於信任產生的人；正式使用要多方儀式" />
    <Gap title="發行、持有、驗證在同一個程序" text="證明了格式與接線，還不是三方互相獨立" />
    <Gap title="沒有爭議流程與託管" text="商店不肯退款時，系統沒有辦法強制" />
    <Gap title="商家沒有身分與信譽" text="名單上的商家自己作假，仍然收得到錢" />
    <Gap title="沒有交易監控（KYT）" text="付款前不查收款方的風險，只靠商家名單" />
    <Gap title="委託與核准是自訂的格式" text="對應的標準草案（AP2、Verifiable Intent）還在 FIDO 制定" />
    <Gap title="送交易的金鑰各只有一把" text="每位客人的付款各自進行，但同一把金鑰的 nonce 一筆一筆給；流量大時要多把" />
  </Frame>
);

// ---------- 19 the order ----------
const Phase = ({ n, title, a, b, now }: { n: string; title: string; a: string; b: string; now?: boolean }) => (
  <div
    style={{
      flex: 1,
      background: now ? ink : panel,
      color: now ? '#fbf7ee' : ink,
      borderRadius: 'var(--osd-radius)',
      padding: '36px 36px 40px',
    }}
  >
    <div style={{ fontFamily: 'var(--osd-font-display)', fontSize: 56, fontStyle: 'italic', color: now ? '#f0a184' : 'var(--osd-accent)', lineHeight: 1 }}>{n}</div>
    <div style={{ fontSize: 38, fontWeight: 800, marginTop: 18 }}>{title}</div>
    <ul style={{ fontSize: 27, lineHeight: 1.5, margin: '20px 0 0', paddingLeft: 30, color: now ? '#d8cfbf' : muted }}>
      <li style={{ marginBottom: 10 }}>{a}</li>
      <li>{b}</li>
    </ul>
  </div>
);

const Order: Page = () => (
  <Frame eyebrow="落地" title="建議的順序：先把信任的起點換成真的">
    <div style={{ display: 'flex', gap: 26, marginTop: 14 }}>
      <Phase now n="01" title="沙盒整合" a="測試網、eKYC 沙盒" b="三個場域通過獨立查證（現況）" />
      <Phase n="02" title="測試網試點" a="商店自己的 x402 端點" b="本人用手機核准" />
      <Phase n="03" title="小額正式" a="eKYC 正式環境、HSM 或 MPC" b="多方產生的 ZK setup" />
      <Phase n="04" title="擴大" a="爭議流程與退款規則" b="KYT、對帳與高流量付款" />
    </div>
    <p style={{ fontSize: 30, color: muted, marginTop: 50, lineHeight: 1.5 }}>
      每一階段都用同一個 checker 驗收：證據包能不能被第三方向鏈重算，是能不能往下走的條件。
    </p>
  </Frame>
);

// ---------- 20 close ----------
const Close: Page = () => (
  <div style={{ ...fill, padding: `0 ${PAD + 30}px`, display: 'flex', flexDirection: 'column', justifyContent: 'center' }}>
    <h2 style={{ fontFamily: 'var(--osd-font-display)', fontSize: 84, fontWeight: 900, lineHeight: 1.3, margin: 0 }}>
      x402 處理付款。
      <br />
      這一層證明這筆錢
      <br />
      <span style={{ color: 'var(--osd-accent)' }}>為什麼可以付。</span>
    </h2>
    <div style={{ marginTop: 70, fontSize: 28, lineHeight: 1.7, color: muted }}>
      架構與威脅模型：docs/architecture.md
      <br />
      與其他方案的分工：docs/positioning.md
      <br />
      身分驗證的接法：docs/commercial-ekyc.md
    </div>
  </div>
);

export const meta: SlideMeta = {
  title: 'Agent Payment Authority：付款架構與落地',
  createdAt: '2026-11-11T13:25:50.021Z',
};

export const notes: (string | undefined)[] = [
  '開場只講一句：AI 可以提出付款，但不能自己批准，也不能自己簽名。這份簡報分兩部分：架構，以及落地要換什麼。',
  '三個擔心是整個設計的出發點，後面每一頁都對得回其中一個。',
  '先把分工講清楚：x402 是付款的管道，這一層是授權與證據。兩者互補，不是取代。x402 在 2026 年 4 月交給 Linux Foundation，Visa、Mastercard、Circle、Google 都是成員。',
  `被問「有沒有照標準」就停在這頁。下面四層都照標準；委託這一層業界還沒有定案，AP2 和 Mastercard 的 Verifiable Intent 都在 FIDO 審，所以用自訂格式，欄位對照在 docs/positioning.md 4.1。
選擇性揭露那一列要照實說：用的是舊版 BBS+，不是 W3C 的 bbs-2023。
底下兩句是重點：模式對得上；差別在 Agent 有沒有金鑰。這裡的 Agent 什麼都簽不了，代價是「這筆是哪個 Agent 送的」靠紀錄，不是密碼學證明。`,
  `從左到右看：本人與 Agent 在左邊，Host 居中，私鑰只在 Signer。
橘色箭頭是付款的路，灰色虛線是事後查證的路。`,
  '重點是「不能做」那一欄：Agent 碰不到金鑰，Host 沒有私鑰，Signer 簽不出委託外的付款。',
  '五步對應展示畫面右側的五步。底下那條是關鍵：所有步驟綁在同一個 intent_hash 上。',
  '規則都是確定性的，模型沒有裁量空間。注意商家名單與頻率上限各擋兩次：Host 被攻破，Signer 那一道還在。',
  '為什麼是兩筆：預留先證明預算夠，結算才動錢。分開追蹤，結果不明時先對帳。',
  '身分只驗一次。之後每間店只拿到它宣告要求的屬性，查帳的第二項會驗「不多不少」。最後一行是台灣業界現在用的 KYH／KYA／KYT 三層：前兩層有，KYT 還沒做。',
  '最後一行很重要：照實說擋不住的部分。自動範圍內的誤購靠上限限制損失。',
  '十項主張可以逐項念，也可以只講 E4 與 E8：向鏈重算，不相信系統自己的紀錄。',
  '進入第二部分：目前是測試網與沙盒，接下來講要換什麼。',
  `這張圖畫的是正式環境，橘黃小字是目前 PoC 的做法。
先看粗橘線：錢只在鏈上從客人的錢包走到商店的錢包，不經過 Agent，也不經過 Host。
黑線是授權：Agent 提出、Host 裁決、Signer 簽名，Relayer 與 Facilitator 把兩筆交易送上鏈。
藍點線是身分資料：證件與自拍只到 eKYC 廠商，店家拿到的只有屬性。
灰虛線是證據：稽核方只讀證據包與鏈。
展示時看不到的 Relayer、Facilitator、發行者、狀態清單與 Checker，都在這張圖上。`,
  `同一張圖，走一筆真的交易：展示裡孩子訂的 0.42 生日蛋糕。每按一次 → 走一步，共九步。
講的時候跟著亮起來的線：藍色是身分，黑色是授權，第七步的橘色硬幣是錢真正移動的那一刻。
重點可以停在第四步和第七步：超過 0.10 所以要問本人；錢只在鏈上從一個錢包到另一個錢包。
最後一步是稽核：前面每一步都留下了可以重算的證據。`,
  `第二筆是出事的情況，跟上一頁對照著講：0.08 在自動範圍內，所以全程沒有人介入就付了。
第三步錢到了郵局；第四步郵局沒交付。關鍵是 Host 不相信商店說了什麼，自己去鏈上查。
第五步是整張圖上唯一需要人判斷的地方：該不該退。第六步硬幣反方向走回客人的錢包，這是第三筆鏈上交易。
要照實說兩個限制：商店不肯退，系統沒有辦法強制；退款不會把任務預算加回去。這個情境目前只在本機鏈跑過。`,
  `第三筆是被擋下的情況，也是 eKYC 和零知識證明最看得出用處的一筆。
前四步講孩子：憑證寫的是未滿 18 歲，所以在付款前就被拒絕，圖上三個紅叉代表沒有預留、沒有簽名、沒有結算。
第五步是對照：家長買同一件商品就放行，這時 ZK 預算才上場。
最後一步收在兩項技術：eKYC 讓年齡不是自己說的；零知識證明讓店家只知道一個是或否，鏈上只看到承諾值。
要照實說的限制：是否滿 18 歲是發行者簽好的一個是或否，不是對生日做範圍證明；ZK 預算只對鏈上隱藏，Relayer 看得到上限與餘額；eKYC 目前接的是沙盒。`,
  '右欄是建議，不是已經做完的事。介面不變，換的是背後的環境。本人核准那列：同步的 passkey 屬 NIST 的 AAL2，大額要綁裝置、不同步的金鑰才到 AAL3。',
  '這一頁是老實話。每一項在 docs/architecture.md 的限制裡都有對應的說明。最後兩項是 11 月新補的：沒有交易監控，以及委託格式是自訂的。',
  '順序的原則：先把信任的起點（身分、金鑰、商店）換成真的，再擴大。每一階段用同一個 checker 驗收。',
  '收在定位句。想看細節的人，三份文件都在 repo 裡。',
];

export default [
  Cover,
  Worries,
  Split,
  Standards,
  Roles,
  Powers,
  FiveSteps,
  PolicyPage,
  TwoTx,
  Identity,
  Threats,
  Checker,
  Section,
  Deploy,
  Walkthrough,
  RefundCase,
  DeniedCase,
  Replace,
  Gaps,
  Order,
  Close,
] satisfies Page[];
