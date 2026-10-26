"use strict";
// Rigscope live control panel: an animated circuit board of this PC, a heat
// gauge, activity graphs and the list of what's using the PC.
// Reads /api/live once a second. Uses helpers from index.html (api, esc, gb, R).

const LV = {
  d: null, hist: [], built: '', sort: 'cpu', filter: 'all', q: '', order: [], orderAt: 0, showAll: false,
  buses: [], fans: [], hdd: null, leds: [], tw: {}, raf: 0, last: 0, reduce: matchMedia('(prefers-reduced-motion: reduce)').matches,
};
const HIST = 120;

/* ---------- formatting ---------- */
function rate(bps){
  if (!(bps > 0)) return '0 KB/s';
  if (bps < 1e6) return Math.round(bps / 1e3) + ' KB/s';
  return (bps / 1e6).toFixed(bps < 1e7 ? 1 : 0) + ' MB/s';
}
function bits(bps){
  const b = (bps || 0) * 8;
  if (b < 1e6) return Math.round(b / 1e3) + ' kbps';
  return (b / 1e6).toFixed(b < 1e7 ? 1 : 0) + ' Mbps';
}
function memSize(b){ return b >= 1e9 ? (b / 2**30).toFixed(1) + ' GB' : Math.round(b / 2**20) + ' MB'; }
function up(s){ const d = Math.floor(s / 86400), h = Math.floor(s % 86400 / 3600), m = Math.floor(s % 3600 / 60); return d ? `${d} d ${h} h` : h ? `${h} h ${m} min` : `${m} min`; }
function lerp(a, b, t){ return a + (b - a) * t; }
function mix(c1, c2, t){
  const p = c => [1, 3, 5].map(i => parseInt(c.slice(i, i + 2), 16));
  const a = p(c1), b = p(c2);
  return '#' + a.map((v, i) => Math.round(lerp(v, b[i], t)).toString(16).padStart(2, '0')).join('');
}
// load or heat (0..1) to a color: deep teal when idle, gold when busy, red when maxed
const HEAT = ['#14524A', '#1F8A7A', '#3FB39A', '#E9C46A', '#F08A4B', '#E5484D'];
function heat(t){
  t = Math.max(0, Math.min(1, t)) * (HEAT.length - 1);
  const i = Math.min(HEAT.length - 2, Math.floor(t));
  return mix(HEAT[i], HEAT[i + 1], t - i);
}
function tempT(c){ return Math.max(0, Math.min(1, (c - 35) / 60)); } // 35°C cool .. 95°C very hot
function tempWord(c){ return c < 0 ? 'Not available' : c < 60 ? 'Cool' : c < 75 ? 'Normal' : c < 85 ? 'Warm' : c < 95 ? 'Hot' : 'Very hot'; }
function ioT(bps, full){ return bps > 0 ? Math.min(1, Math.log10(1 + bps / 1e3) / Math.log10(1 + full / 1e3)) : 0; }

/* ---------- board geometry ---------- */
const W = 1200, H = 560;
const CPU = {x: 480, y: 150, w: 240, h: 240};

function poly(pts){ return 'M' + pts.map(p => p[0].toFixed(1) + ' ' + p[1].toFixed(1)).join('L'); }
// horizontal, then a 45 degree diagonal, then horizontal: how PCB traces run
function route(ax, ay, bx, by, bend){
  const dir = Math.sign(bx - ax) || 1, dy = Math.abs(by - ay);
  let x1 = bend, x2 = bend + dir * dy;
  if ((x2 - bx) * dir > -8) { x2 = bx - dir * 8; x1 = x2 - dir * dy; }
  return [[ax, ay], [x1, ay], [x2, by], [bx, by]];
}
function measure(pts){
  const seg = []; let total = 0;
  for (let i = 1; i < pts.length; i++) { const l = Math.hypot(pts[i][0] - pts[i-1][0], pts[i][1] - pts[i-1][1]); seg.push(l); total += l; }
  return {pts, seg, total};
}
function pointAt(m, d){
  d = ((d % m.total) + m.total) % m.total;
  for (let i = 0; i < m.seg.length; i++) {
    if (d <= m.seg[i]) { const t = m.seg[i] ? d / m.seg[i] : 0; return [lerp(m.pts[i][0], m.pts[i+1][0], t), lerp(m.pts[i][1], m.pts[i+1][1], t)]; }
    d -= m.seg[i];
  }
  return m.pts[m.pts.length - 1];
}

function layout(){
  const rep = window.R || null, d = LV.d;
  const laptop = rep ? rep.laptop : !!(d && d.power && d.power.hasBattery);
  const slots = rep ? Math.min(4, Math.max(rep.memory.slots || 0, rep.memory.modules.length, 1)) : 2;
  const mods = rep ? rep.memory.modules : [];
  const g0 = rep && rep.gpus[0];
  const integrated = g0 ? g0.integrated : !!(d && d.gpu.shared);
  const sysDisk = rep && (rep.disks.find(x => x.letters.includes('C:')) || rep.disks[0]);
  const hdd = sysDisk && sysDisk.kind === 'HDD';
  const threads = d ? d.cpu.cores.length : (rep ? rep.cpu.threads : 4);
  return {laptop, slots, mods, integrated, hdd, threads, gpuName: (g0 && g0.name) || (d && d.gpu.name) || 'Graphics', diskName: (sysDisk && (sysDisk.name || sysDisk.model)) || 'System drive',
    cpuName: rep ? prettyCPU(rep.cpu.name) : 'Processor', key: [laptop, slots, mods.length, integrated, hdd, threads, !!rep].join('|')};
}

/* ---------- drawing the board ---------- */
function buildBoard(){
  const L = layout(), el = $('#boardsvg');
  LV.built = L.key; LV.buses = []; LV.fans = []; LV.leds = [];
  const first = !el.dataset.drawn;
  const s = [];
  const anim = (cls, delay) => first && !LV.reduce ? ` class="${cls}" style="--d:${delay}s"` : '';

  // background: vias, mounting holes, silkscreen
  s.push(`<defs>
    <pattern id="vias" width="24" height="24" patternUnits="userSpaceOnUse"><circle cx="3" cy="3" r="1.1" fill="#2E5C53" opacity=".55"/></pattern>
    <radialGradient id="heatglow"><stop offset="0" stop-color="#F08A4B" stop-opacity=".55"/><stop offset=".55" stop-color="#F08A4B" stop-opacity=".12"/><stop offset="1" stop-color="#F08A4B" stop-opacity="0"/></radialGradient>
    <linearGradient id="lid" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#2A3644"/><stop offset="1" stop-color="#151C25"/></linearGradient>
    <linearGradient id="shroud" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#1C252F"/><stop offset="1" stop-color="#10161D"/></linearGradient>
    <filter id="soft" x="-50%" y="-50%" width="200%" height="200%"><feGaussianBlur stdDeviation="2.2"/></filter>
  </defs>
  <rect width="${W}" height="${H}" fill="url(#vias)"/>
  ${[[22,22],[W-22,22],[22,H-22],[W-22,H-22]].map(([x,y]) => `<circle cx="${x}" cy="${y}" r="9" fill="#071A17" stroke="var(--pad)" stroke-width="3" opacity=".8"/>`).join('')}
  <text class="silk" x="44" y="30">RIGSCOPE LIVE  REV 1.1</text>
  <text class="silk" x="44" y="${H-20}">${esc(L.cpuName.toUpperCase())}</text>`);

  // heat glow behind the processor
  s.push(`<circle id="glow" class="glow" cx="${CPU.x + CPU.w/2}" cy="${CPU.y + CPU.h/2}" r="230" fill="url(#heatglow)" opacity="0"/>`);

  // traces
  const bus = (id, lines, dirs, delay) => {
    lines.forEach((pts, i) => {
      const m = measure(pts);
      s.push(`<path class="tr${first && !LV.reduce ? ' draw' : ''}" style="--len:${m.total.toFixed(0)};--d:${(delay + i * 0.05).toFixed(2)}s" d="${poly(pts)}"/>`);
      LV.buses.push({id, m, dir: dirs[i], speed: 0, alpha: 0, pk: [Math.random() * m.total, Math.random() * m.total], el: null});
    });
  };
  const ramMid = 70 + L.slots * 25, ssdMid = 410, gpuMid = 150, netMid = 400;
  bus('ram', [0,1,2,3].map(i => route(CPU.x, 186 + i*13, 362, ramMid - 18 + i*12, 452 - i*7)), [1,-1,1,-1], .15);
  bus('ssd', [0,1,2,3].map(i => route(CPU.x, 302 + i*13, 362, ssdMid - 18 + i*12, 452 - (3-i)*7)), [-1,-1,1,1], .3);
  bus('gpu', [0,1,2,3].map(i => route(CPU.x + CPU.w, 186 + i*13, 838, gpuMid - 18 + i*12, 748 + i*7)), [1,-1,1,-1], .2);
  bus('net', [0,1,2,3].map(i => route(CPU.x + CPU.w, 302 + i*13, 838, netMid - 18 + i*12, 748 + (3-i)*7)), [-1,-1,1,1], .35);
  const pwrTop = L.laptop ? 478 : 500;
  bus('pwr', [0,1,2].map(i => { const sx = 632 + i*14, y = 548 - i*9, ex = 1090 - i*14; return [[sx, CPU.y + CPU.h], [sx, y], [ex, y], [ex, pwrTop]]; }), [-1,-1,-1], .45);

  // processor package
  const cx = CPU.x, cy = CPU.y;
  let pins = '';
  for (let i = 0; i < 11; i++) {
    const p = 22 + i * 19.6;
    pins += `<rect x="${cx + p - 3}" y="${cy - 10}" width="6" height="12" rx="1.5"/><rect x="${cx + p - 3}" y="${cy + CPU.h - 2}" width="6" height="12" rx="1.5"/><rect x="${cx - 10}" y="${cy + p - 3}" width="12" height="6" rx="1.5"/><rect x="${cx + CPU.w - 2}" y="${cy + p - 3}" width="12" height="6" rx="1.5"/>`;
  }
  const n = Math.max(1, L.threads);
  let cols = Math.ceil(Math.sqrt(n));
  for (let c = cols; c <= n; c++) if (n % c === 0 && c <= 2 * (n / c)) { cols = c; break; }
  const rows = Math.ceil(n / cols);
  const gx = cx + 34, gy = cy + 34, gw = CPU.w - 68, gh = CPU.h - 68, gap = n > 16 ? 4 : 7;
  const cw = (gw - (cols - 1) * gap) / cols, ch = (gh - (rows - 1) * gap) / rows;
  let cells = '';
  for (let i = 0; i < n; i++) {
    const x = gx + (i % cols) * (cw + gap), y = gy + Math.floor(i / cols) * (ch + gap);
    cells += `<g><title id="cpt${i}">Thread ${i + 1}</title><rect id="core${i}" class="core" x="${x.toFixed(1)}" y="${y.toFixed(1)}" width="${cw.toFixed(1)}" height="${ch.toFixed(1)}" rx="${Math.min(8, cw / 5).toFixed(1)}" fill="#14524A"/>
      ${cw > 34 && ch > 26 ? `<text id="cpv${i}" x="${(x + cw/2).toFixed(1)}" y="${(y + ch/2 + 5).toFixed(1)}" text-anchor="middle" font-size="${Math.min(15, cw / 3.6).toFixed(1)}" font-weight="600" fill="#F2F7F5" opacity=".9">0%</text>` : ''}</g>`;
  }
  s.push(`<g${anim('fadein', .1)}><g fill="var(--pad)">${pins}</g>
    <rect x="${cx}" y="${cy}" width="${CPU.w}" height="${CPU.h}" rx="18" fill="url(#lid)" stroke="#3A4A5C"/>
    <rect x="${cx + 22}" y="${cy + 22}" width="${CPU.w - 44}" height="${CPU.h - 44}" rx="10" fill="#0B1118" stroke="#2B3A4A"/>
    ${cells}
    <text class="silk" x="${cx + 4}" y="${cy + CPU.h + 30}">U1  ${n} THREADS</text></g>`);

  // readout plate above the processor
  s.push(`<g${anim('fadein', .2)}>
    <rect x="${cx - 75}" y="36" width="${CPU.w + 150}" height="82" rx="12" fill="#06110F" stroke="#1E4840"/>
    <text class="lbl" x="${cx - 52}" y="62">Temperature</text><text class="big" id="b-temp" x="${cx - 52}" y="101">--</text>
    <text class="lbl" x="${cx + 86}" y="62">Speed</text><text class="big" id="b-mhz" x="${cx + 86}" y="101">--</text>
    <text class="lbl" x="${cx + 246}" y="62">Load</text><text class="big" id="b-load" x="${cx + 246}" y="101">--</text></g>`);

  // memory sticks
  let ram = `<text class="silk" x="60" y="56">DIMM</text>`;
  for (let i = 0; i < L.slots; i++) {
    const y = 70 + i * 50, mod = L.mods[i];
    if (!mod && window.R) { ram += `<rect x="60" y="${y}" width="300" height="34" rx="5" fill="none" stroke="#2E5C53" stroke-dasharray="6 5"/><text class="lbl" x="210" y="${y + 22}" text-anchor="middle">Empty slot</text>`; continue; }
    let chips = '';
    for (let c = 0; c < 8; c++) chips += `<rect id="rc${i}_${c}" class="chiplit" x="${74 + c * 35}" y="${y + 6}" width="28" height="16" rx="2.5" fill="#0B1118"/>`;
    let teeth = '';
    for (let x = 66; x < 356; x += 6) if (Math.abs(x - 186) > 5) teeth += `<rect x="${x}" y="${y + 28}" width="3.4" height="6" rx=".8"/>`;
    ram += `<rect x="60" y="${y}" width="300" height="34" rx="4" fill="#0F4A40" stroke="#1E6A5D"/>${chips}<g fill="var(--pad)">${teeth}</g>
      ${mod ? `<text class="silk" x="${356}" y="${y - 4}" text-anchor="end" opacity=".45">${esc(gb(mod.capacity))} ${esc(mod.typeName || '')}</text>` : ''}`;
  }
  const ry = 70 + L.slots * 50 + 8;
  ram += `<text class="lbl" x="60" y="${ry + 12}">Memory</text><text class="val" id="b-mem" x="60" y="${ry + 32}">--</text>`;
  s.push(`<g${anim('fadein', .25)}>${ram}</g>`);

  // system drive
  let drv;
  if (L.hdd) {
    drv = `<rect x="60" y="370" width="300" height="80" rx="8" fill="#1B232C" stroke="#33414F"/>
      <circle id="platter" cx="140" cy="410" r="32" fill="#2A3542" stroke="#4A5A6C"/><circle cx="140" cy="410" r="6" fill="#4A5A6C"/>
      <path id="arm" d="M232 438 L160 412" stroke="#9AA8B8" stroke-width="5" stroke-linecap="round"/>`;
  } else {
    let nand = '';
    for (let i = 0; i < 3; i++) nand += `<rect x="${170 + i * 58}" y="390" width="50" height="34" rx="3" fill="#0B1118"/>`;
    let gold = '';
    for (let y = 388; y < 428; y += 5) gold += `<rect x="60" y="${y}" width="10" height="3"/>`;
    drv = `<rect x="64" y="382" width="296" height="50" rx="5" fill="#0F4A40" stroke="#1E6A5D"/><g fill="var(--pad)">${gold}</g>
      <rect x="92" y="390" width="58" height="34" rx="3" fill="#18222C"/><text x="121" y="411" text-anchor="middle" font-size="9" fill="#7E93A6">CTRL</text>${nand}
      <circle cx="350" cy="407" r="6" fill="#071A17" stroke="var(--pad)" stroke-width="2"/>`;
  }
  s.push(`<g${anim('fadein', .35)}><text class="silk" x="60" y="364">${L.hdd ? 'SATA_HDD' : 'M.2_SSD'}  ${esc(L.diskName.toUpperCase().slice(0, 30))}</text>${drv}
    <circle id="led-disk" class="led" cx="${L.hdd ? 340 : 80}" cy="${L.hdd ? 388 : 444}" r="4.5" fill="#F0B43C" opacity=".15"/>
    <text class="lbl" x="60" y="476">Disk</text><text class="val" id="b-disk" x="60" y="497">--</text></g>`);

  // graphics
  let gfx;
  if (L.integrated) {
    gfx = `<rect x="840" y="70" width="300" height="150" rx="12" fill="#121A22" stroke="#2B3A4A"/>
      <text class="lbl" x="860" y="98">Built into the processor</text>
      <text class="val" x="860" y="122">${esc(L.gpuName.replace(/\(R\)|\(TM\)/g, '').slice(0, 30))}</text>
      ${[0,1].map(i => `<rect x="${862 + i * 74}" y="148" width="58" height="26" rx="4" fill="#0B1118" stroke="#3A4A5C"/><rect x="${872 + i * 74}" y="156" width="38" height="10" rx="2" fill="#2B3A4A"/>`).join('')}
      <text class="silk" x="1010" y="166">HDMI  USB-C</text>
      <rect x="860" y="190" width="260" height="8" rx="4" fill="#0B1118"/><rect id="gbar" x="860" y="190" width="0" height="8" rx="4" fill="#3FB39A"/>`;
  } else {
    const fan = (fx, id) => {
      let blades = '';
      for (let b = 0; b < 9; b++) blades += `<path d="M0 -8 C 12 -14, 26 -22, 34 -36 C 22 -32, 10 -26, 4 -12 Z" transform="rotate(${b * 40})" fill="#3A4756"/>`;
      return `<g transform="translate(${fx} 145)"><circle r="47" fill="#0B1118" stroke="#2B3A4A" stroke-width="2"/><g id="${id}">${blades}</g><circle r="11" fill="#1C252F" stroke="#3A4A5C"/></g>`;
    };
    gfx = `<rect x="836" y="232" width="236" height="12" rx="2" fill="var(--pad)" opacity=".9"/>
      <rect x="832" y="66" width="312" height="168" rx="14" fill="url(#shroud)" stroke="#33414F"/>
      <rect x="832" y="66" width="312" height="18" rx="9" fill="#1E2833"/><text class="silk" x="848" y="79" opacity=".8">${esc(L.gpuName.replace(/NVIDIA |AMD |\(R\)|\(TM\)/g, '').toUpperCase().slice(0, 34))}</text>
      ${fan(920, 'fan0')}${fan(1056, 'fan1')}`;
    LV.fans.push({id: 'fan0', a: 0, rpm: 0}, {id: 'fan1', a: 40, rpm: 0});
  }
  s.push(`<g${anim('fadein', .3)}><text class="silk" x="840" y="56">${L.integrated ? 'GFX' : 'PCIE_X16'}</text>${gfx}
    <text class="lbl" x="840" y="268">Graphics</text><text class="val" id="b-gpu" x="840" y="289">--</text></g>`);

  // network
  s.push(`<g${anim('fadein', .4)}><text class="silk" x="840" y="356">LAN / WLAN</text>
    <rect x="840" y="366" width="130" height="82" rx="8" fill="#B9C3CC"/><rect x="852" y="378" width="106" height="58" rx="4" fill="#20282F"/>
    <path d="M876 436 V412 H886 V404 H924 V412 H934 V436 Z" fill="#0B1118"/>
    ${[0,1,2,3,4,5,6,7].map(i => `<rect x="${885 + i * 5}" y="406" width="2.6" height="7" fill="var(--pad)"/>`).join('')}
    <rect id="led-net0" class="led" x="856" y="381" width="12" height="6" rx="2" fill="#5BE38F" opacity=".9"/><rect id="led-net1" class="led" x="942" y="381" width="12" height="6" rx="2" fill="#F0B43C" opacity=".15"/>
    <text class="lbl" x="840" y="476">Network</text><text class="val" id="b-net" x="840" y="497">--</text><text class="val" id="b-net2" x="840" y="517"></text></g>`);

  // battery or cooling fan
  if (L.laptop) {
    s.push(`<g${anim('fadein', .45)}><text class="silk" x="1010" y="356">BAT1</text>
      <text class="lbl" x="1010" y="378">Battery</text><text class="val" id="b-bat" x="1010" y="398">--</text>
      <rect x="1010" y="408" width="130" height="64" rx="10" fill="none" stroke="#CFE4DD" stroke-width="3" opacity=".85"/><rect x="1141" y="427" width="9" height="26" rx="3" fill="#CFE4DD" opacity=".85"/>
      <rect id="batfill" x="1017" y="415" width="0" height="50" rx="6" fill="#3FB39A"/>
      <path id="bolt" d="M1078 421 L1062 443 H1074 L1068 463 L1088 437 H1076 Z" fill="#F7FBF9" opacity="0"/></g>`);
  } else {
    let blades = '';
    for (let b = 0; b < 7; b++) blades += `<path d="M0 -9 C 14 -16, 30 -26, 40 -42 C 26 -38, 12 -30, 4 -13 Z" transform="rotate(${b * 51.4})" fill="#3A4756"/>`;
    s.push(`<g${anim('fadein', .45)}><text class="silk" x="1010" y="356">FAN1</text>
      <text class="lbl" x="1010" y="378">Cooling</text><text class="val" id="b-fan" x="1010" y="398">--</text>
      <rect x="1018" y="406" width="92" height="92" rx="12" fill="#121A22" stroke="#2B3A4A"/>
      <g transform="translate(1064 452)"><circle r="40" fill="#0B1118"/><g id="fan2" transform="scale(.78)">${blades}</g><circle r="10" fill="#1C252F" stroke="#3A4A5C"/></g></g>`);
    LV.fans.push({id: 'fan2', a: 0, rpm: 0});
  }

  // packets (drawn last so they sit on top of the traces)
  let pk = '';
  LV.buses.forEach((b, i) => { pk += `<circle id="pk${i}a" r="3.2" fill="#FFE7A3" opacity="0"/><circle id="pk${i}b" r="3.2" fill="#FFE7A3" opacity="0"/>`; });
  s.push(`<g filter="url(#soft)" opacity=".9">${pk.replace(/id="pk/g, 'id="pg')}</g><g>${pk}</g>`);

  el.innerHTML = s.join('');
  el.dataset.drawn = '1';
  LV.buses.forEach((b, i) => { b.el = [$('#pk' + i + 'a'), $('#pk' + i + 'b')]; b.gl = [$('#pg' + i + 'a'), $('#pg' + i + 'b')]; });
  LV.fanEls = LV.fans.map(f => ({...f, el: document.getElementById(f.id)}));
}

/* ---------- updating the board ---------- */
function setText(id, t){ const e = document.getElementById(id); if (e && e.textContent !== t) e.textContent = t; }

function updateBoard(d){
  const L = layout();
  if (LV.built !== L.key) buildBoard();
  d.cpu.cores.forEach((v, i) => {
    const c = document.getElementById('core' + i); if (!c) return;
    c.setAttribute('fill', heat(v / 100));
    setText('cpv' + i, Math.round(v) + '%');
    const t = d.temp.cores[Math.floor(i / Math.max(1, d.cpu.cores.length / Math.max(1, d.temp.cores.length)))];
    setText('cpt' + i, `Thread ${i + 1}: ${Math.round(v)}% busy${t ? `, ${t.name} ${Math.round(t.value)}°C` : ''}`);
  });
  LV.tw.temp = d.temp.cpu; LV.tw.mhz = d.cpu.mhz; LV.tw.load = d.cpu.total;
  const g = $('#glow');
  if (g) g.setAttribute('opacity', d.temp.cpu > 0 ? (0.15 + tempT(d.temp.cpu) * 0.85).toFixed(2) : (0.1 + d.cpu.total / 200).toFixed(2));

  // memory chips light up with use
  const mfrac = d.mem.total ? d.mem.used / d.mem.total : 0;
  const installed = []; document.querySelectorAll('[id^="rc"]').forEach(e => installed.push(e));
  const lit = Math.round(mfrac * installed.length);
  installed.forEach((e, i) => e.setAttribute('fill', i < lit ? heat(0.38 + 0.55 * mfrac) : '#0B1118'));
  setText('b-mem', `${memSize(d.mem.used)} of ${memSize(d.mem.total)}  ${Math.round(mfrac * 100)}%`);

  setText('b-disk', d.disk.ok ? `Read ${rate(d.disk.in)}  Write ${rate(d.disk.out)}` : 'No activity data');
  setText('b-net', d.net.ok ? `${bits(d.net.in)} down` : 'No activity data'); setText('b-net2', d.net.ok ? `${bits(d.net.out)} up` : '');
  const gpuTxt = d.gpu.util >= 0 ? `${Math.round(d.gpu.util)}% busy` : 'Load not reported';
  const vram = d.gpu.vramUsed >= 0 ? (d.gpu.vramTotal > 0 ? `  ${memSize(d.gpu.vramUsed)} of ${memSize(d.gpu.vramTotal)}` : `  ${memSize(d.gpu.vramUsed)} used`) : '';
  setText('b-gpu', gpuTxt + vram + (d.temp.gpu > 0 ? `  ${Math.round(d.temp.gpu)}°C` : ''));
  const gb_ = $('#gbar'); if (gb_) gb_.setAttribute('width', (Math.max(0, d.gpu.util) / 100 * 260).toFixed(1));

  // battery
  const p = d.power;
  if ($('#batfill') && p.percent >= 0) {
    $('#batfill').setAttribute('width', (116 * p.percent / 100).toFixed(1));
    $('#batfill').setAttribute('fill', p.percent < 20 ? '#E5484D' : p.percent < 40 ? '#E9C46A' : '#3FB39A');
    $('#bolt').setAttribute('opacity', p.charging ? '1' : '0');
    const w = d.power.watts;
    setText('b-bat', `${p.percent}%  ${p.acOnline ? (p.charging ? 'Charging' : 'Plugged in') : 'On battery'}${w ? '  ' + Math.abs(w).toFixed(1) + ' W' : ''}`);
  }
  const cpuFan = d.temp.fans.find(f => /cpu/i.test(f.name)) || d.temp.fans[0];
  setText('b-fan', cpuFan ? `${cpuFan.name}  ${Math.round(cpuFan.value).toLocaleString()} RPM` : `Fan speed not reported`);

  // traffic on the traces
  const act = {
    ram: Math.min(1, d.cpu.total / 100 + mfrac * 0.15),
    gpu: d.gpu.util >= 0 ? d.gpu.util / 100 : 0.1,
    pwr: d.power.watts ? Math.min(1, Math.abs(d.power.watts) / 60) : (d.cpu.power > 0 ? Math.min(1, d.cpu.power / 120) : 0.15 + d.cpu.total / 300),
  };
  LV.buses.forEach(b => {
    let a = act[b.id];
    if (b.id === 'ssd') a = ioT(b.dir < 0 ? d.disk.in : d.disk.out, 4e8);
    if (b.id === 'net') a = ioT(b.dir < 0 ? d.net.in : d.net.out, 1.2e7);
    a = Math.max(0, Math.min(1, a || 0));
    b.target = a;
  });
  // fans: real RPM when known, otherwise follow load
  LV.fanEls && LV.fanEls.forEach(f => {
    if (f.id === 'fan2') f.rpm = cpuFan ? cpuFan.value : 500 + d.cpu.total * 14;
    else f.rpm = d.gpu.fanPct >= 0 ? 300 + d.gpu.fanPct * 25 : (d.gpu.util >= 0 ? d.gpu.util * 20 : 0);
  });
  const ld = $('#led-disk'); if (ld) LV.leds.disk = d.disk.ok ? Math.min(1, (d.disk.active > 0 ? d.disk.active / 40 : ioT(d.disk.in + d.disk.out, 1e8))) : 0;
  LV.leds.net = d.net.ok ? ioT(d.net.in + d.net.out, 5e6) : 0;
}

function frame(ts){
  LV.raf = requestAnimationFrame(frame);
  const dt = LV.last ? Math.min(0.1, (ts - LV.last) / 1000) : 0.016;
  LV.last = ts;
  if (document.hidden || LV.view !== 'live') return;
  // numbers ease toward their new values
  const ease = 1 - Math.pow(0.001, dt);
  for (const k of ['temp', 'mhz', 'load']) {
    const t = LV.tw[k]; if (t == null) continue;
    const cur = LV.tw['_' + k] == null ? t : LV.tw['_' + k];
    const v = LV.reduce ? t : lerp(cur, t, ease);
    LV.tw['_' + k] = v;
    if (k === 'temp') setText('b-temp', t > 0 ? Math.round(v) + '°C' : '--');
    if (k === 'mhz') setText('b-mhz', t > 0 ? (v / 1000).toFixed(2) + ' GHz' : '--');
    if (k === 'load') setText('b-load', Math.round(v) + '%');
  }
  const bt = $('#b-temp'); if (bt && LV.tw.temp > 0) bt.setAttribute('fill', LV.tw.temp >= 85 ? '#FF9A84' : LV.tw.temp >= 75 ? '#F6C177' : '#F7FBF9');
  if (LV.reduce) return;
  // data packets
  LV.buses.forEach(b => {
    b.alpha = lerp(b.alpha, b.target || 0, Math.min(1, dt * 3));
    const speed = 30 + b.alpha * 420;
    [0, 1].forEach(k => {
      b.pk[k] += b.dir * speed * dt * (k ? 0.82 : 1);
      const [x, y] = pointAt(b.m, b.pk[k]);
      const o = (0.08 + b.alpha * 0.92).toFixed(2);
      for (const e of [b.el[k], b.gl[k]]) if (e) { e.setAttribute('cx', x.toFixed(1)); e.setAttribute('cy', y.toFixed(1)); e.setAttribute('opacity', o); }
    });
  });
  // fans and activity lights
  LV.fanEls && LV.fanEls.forEach(f => { if (!f.el) return; f.a = (f.a + f.rpm / 60 * 360 * dt * 0.25) % 360; f.el.setAttribute('transform', `rotate(${f.a.toFixed(1)})${f.id === 'fan2' ? ' scale(.78)' : ''}`); });
  const flick = (id, level) => { const e = document.getElementById(id); if (e) e.setAttribute('opacity', (level > 0.02 && Math.random() < 0.35 + level * 0.6 ? 0.35 + level * 0.65 : 0.12).toFixed(2)); };
  if (Math.random() < 0.5) { flick('led-disk', LV.leds.disk || 0); flick('led-net1', LV.leds.net || 0); }
}

/* ---------- heat card ---------- */
function gaugeSVG(c){
  const r = 84, cx = 110, cy = 104, A = Math.PI;
  const pt = (t, rr) => [cx + Math.cos(A + t * Math.PI) * rr, cy + Math.sin(A + t * Math.PI) * rr];
  let ticks = '';
  for (let v = 30; v <= 100; v += 10) { const t = (v - 30) / 70, [x1, y1] = pt(t, r + 10), [x2, y2] = pt(t, r + 16), [tx, ty] = pt(t, r + 27);
    ticks += `<line x1="${x1.toFixed(1)}" y1="${y1.toFixed(1)}" x2="${x2.toFixed(1)}" y2="${y2.toFixed(1)}" stroke="var(--line2)" stroke-width="2"/><text x="${tx.toFixed(1)}" y="${(ty + 4).toFixed(1)}" text-anchor="middle" font-size="10.5" fill="var(--faint)">${v}</text>`; }
  const ok = c > 0, t = ok ? Math.max(0, Math.min(1, (c - 30) / 70)) : 0;
  const len = Math.PI * r;
  const [nx, ny] = pt(t, r - 22);
  return `<svg class="gauge" viewBox="-14 -8 248 150" role="img" aria-label="Processor temperature ${ok ? Math.round(c) + ' degrees' : 'not available'}">
    <defs><linearGradient id="gg" x1="0" x2="1"><stop offset="0" stop-color="#1F8A7A"/><stop offset=".45" stop-color="#3FB39A"/><stop offset=".68" stop-color="#E9C46A"/><stop offset=".85" stop-color="#F08A4B"/><stop offset="1" stop-color="#E5484D"/></linearGradient></defs>
    <path d="M${cx - r} ${cy} A ${r} ${r} 0 0 1 ${cx + r} ${cy}" fill="none" stroke="var(--line)" stroke-width="14" stroke-linecap="round"/>
    <path d="M${cx - r} ${cy} A ${r} ${r} 0 0 1 ${cx + r} ${cy}" fill="none" stroke="url(#gg)" stroke-width="14" stroke-linecap="round" stroke-dasharray="${(len * t).toFixed(1)} ${len.toFixed(1)}" style="transition:stroke-dasharray .8s"/>
    ${ticks}
    ${ok ? `<line x1="${cx}" y1="${cy}" x2="${nx.toFixed(1)}" y2="${ny.toFixed(1)}" stroke="var(--ink)" stroke-width="3" stroke-linecap="round"/><circle cx="${cx}" cy="${cy}" r="6" fill="var(--ink)"/>` : ''}
    <text x="${cx}" y="${cy + 30}" text-anchor="middle" font-size="26" font-weight="600" fill="var(--ink)">${ok ? Math.round(c) + '°C' : '--'}</text></svg>`;
}

function chartSVG(series, opts){
  const w = 560, h = opts.h || 120, pad = 4;
  const longest = Math.max(...series.map(s => s.data.length), 2);
  const n = Math.max(30, Math.min(HIST, longest)), max = opts.max, min = opts.min || 0;
  const pts = (arr) => arr.map((v, i) => [pad + (w - 2 * pad) * (i + n - arr.length) / (n - 1), h - pad - (h - 2 * pad) * (Math.max(min, Math.min(max, v)) - min) / (max - min)]);
  let out = '';
  (opts.grid || []).forEach(g => { const y = h - pad - (h - 2 * pad) * (g - min) / (max - min); out += `<line x1="0" x2="${w}" y1="${y.toFixed(1)}" y2="${y.toFixed(1)}" stroke="var(--line)"/><text x="${w - 2}" y="${(y - 3).toFixed(1)}" text-anchor="end">${g}${opts.unit || ''}</text>`; });
  series.forEach(s => {
    const vals = s.data.filter(v => v != null && v >= 0); if (vals.length < 2) return;
    const p = pts(s.data.map(v => v == null || v < 0 ? 0 : v));
    const line = poly(p);
    if (s.area) out += `<path d="${line}L${p[p.length-1][0].toFixed(1)} ${h}L${p[0][0].toFixed(1)} ${h}Z" fill="${s.color}" opacity=".14"/>`;
    out += `<path d="${line}" fill="none" stroke="${s.color}" stroke-width="${s.width || 2}" stroke-linejoin="round" ${s.dash ? 'stroke-dasharray="4 4"' : ''}/>`;
  });
  return `<svg viewBox="0 0 ${w} ${h}" preserveAspectRatio="none" class="${opts.cls || ''}" role="img" aria-label="${esc(opts.label || '')}">${out}</svg>`;
}

function renderHeat(d){
  const t = d.temp, c = t.cpu, lim = d.cpu.limit;
  const src = t.source === 'sensor' ? `Exact processor temperature from ${esc(d.sources[0] || 'your sensor app')}.`
    : t.source === 'thermalzone' ? `From Windows' thermal sensor near the processor, which can read a few degrees off. For exact core temperatures, run <a href="https://github.com/LibreHardwareMonitor/LibreHardwareMonitor" target="_blank" rel="noopener">LibreHardwareMonitor</a> and Rigscope will use it automatically.`
    : `Windows doesn't share a processor temperature on this PC without a hardware driver. Run <a href="https://github.com/LibreHardwareMonitor/LibreHardwareMonitor" target="_blank" rel="noopener">LibreHardwareMonitor</a> alongside Rigscope to see it.`;
  const throttle = lim < 0 ? 'Not reported' : lim >= 95 ? 'Running at full speed' : `Slowed to ${Math.round(lim)}% to stay cool or save power`;
  const chips = t.cores.map(r => `<span class="tchip">${esc(r.name)} <b>${Math.round(r.value)}°</b></span>`).join('') +
    (t.gpu > 0 ? `<span class="tchip">Graphics <b>${Math.round(t.gpu)}°</b></span>` : '') +
    t.other.map(r => `<span class="tchip">${esc(r.name)} <b>${Math.round(r.value)}°</b></span>`).join('') +
    t.fans.map(r => `<span class="tchip">${esc(r.name)} <b>${Math.round(r.value).toLocaleString()} RPM</b></span>`).join('') +
    (t.source !== 'sensor' ? t.zones.map(z => `<span class="tchip">Zone ${esc(z.name)} <b>${Math.round(z.value)}°</b></span>`).join('') : '');
  const temps = LV.hist.map(x => x.temp.cpu), loads = LV.hist.map(x => x.cpu.total);
  $('#heat').innerHTML = `<h2>Processor heat ${pill(c < 0 ? 'unknown' : c < 75 ? 'good' : c < 90 ? 'warn' : 'bad', tempWord(c))}</h2>
    <div class="heat">${gaugeSVG(c)}
      <div class="heatfacts">
        <div class="row"><span>Speed limit</span><b>${throttle}</b></div>
        <div class="row"><span>Processor power</span><b>${d.cpu.power > 0 ? d.cpu.power.toFixed(1) + ' W' : 'Not reported'}</b></div>
        <div class="row"><span>Hottest in last 2 minutes</span><b>${Math.max(...temps.filter(v => v > 0), -1) > 0 ? Math.round(Math.max(...temps.filter(v => v > 0))) + '°C' : '--'}</b></div>
        <div class="row"><span>What's normal</span><b>Under 80°C under load</b></div>
      </div></div>
    ${chips ? `<div class="chips">${chips}</div>` : ''}
    <div class="hist">${chartSVG([{data: loads, color: '#3FB39A', area: true, width: 1.5}, ...(c > 0 ? [{data: temps, color: '#F08A4B', width: 2.4}] : [])], {max: 100, grid: [25, 50, 75], label: 'Temperature and load over the last two minutes'})}
      <div class="srcnote" style="margin-top:4px"><span style="color:#F08A4B;font-weight:600">Temperature</span> and <span style="color:var(--good);font-weight:600">load</span>, last 2 minutes</div></div>
    <div class="srcnote">${src}</div>`;
}

/* ---------- activity card ---------- */
function renderActivity(d){
  const h = LV.hist;
  const row = (k, sub, v, vs, data, max, color) => `<div class="arow"><div class="k">${k}<small>${sub}</small></div><div class="v">${v}<small>${vs}</small></div>
    ${chartSVG([{data, color, area: true, width: 1.8}], {max, h: 40, cls: 'spark', label: k + ' over the last two minutes'})}</div>`;
  const netMax = Math.max(1e5, ...h.map(x => x.net.in + x.net.out)), diskMax = Math.max(1e6, ...h.map(x => x.disk.in + x.disk.out));
  $('#activity').innerHTML = `<h2>Activity</h2><p class="lead">Live, updated every second.</p><div class="act">
    ${row('Processor', `${d.cpu.cores.length} threads`, Math.round(d.cpu.total) + '%', d.cpu.mhz > 0 ? (d.cpu.mhz / 1000).toFixed(2) + ' GHz' : '', h.map(x => x.cpu.total), 100, '#3FB39A')}
    ${row('Memory', memSize(d.mem.total), Math.round(d.mem.used / d.mem.total * 100) + '%', memSize(d.mem.used) + ' in use', h.map(x => x.mem.used / x.mem.total * 100), 100, '#7C8CF0')}
    ${row('Graphics', esc((d.gpu.name || '').replace(/\(R\)|\(TM\)|NVIDIA |AMD |Intel /g, '').slice(0, 18)), d.gpu.util >= 0 ? Math.round(d.gpu.util) + '%' : '--', d.gpu.vramUsed >= 0 ? memSize(d.gpu.vramUsed) + (d.gpu.shared ? ' shared' : ' video memory') : '', h.map(x => Math.max(0, x.gpu.util)), 100, '#E9C46A')}
    ${row('Disk', d.disk.active >= 0 ? Math.round(d.disk.active) + '% busy' : 'All drives', d.disk.ok ? rate(d.disk.in + d.disk.out) : '--', d.disk.ok ? `${rate(d.disk.in)} read` : 'Not reported', h.map(x => x.disk.in + x.disk.out), diskMax, '#F08A4B')}
    ${row('Network', 'All connections', d.net.ok ? bits(d.net.in + d.net.out) : '--', d.net.ok ? `${bits(d.net.in)} down` : 'Not reported', h.map(x => x.net.in + x.net.out), netMax, '#5FA8E8')}
  </div>`;
}

/* ---------- what's using the PC ---------- */
const KINDS = {app: 'App', background: 'Background', windows: 'Windows', self: 'This app'};
function monoColor(s){ let h = 0; for (const c of s) h = (h * 31 + c.charCodeAt(0)) >>> 0; return `hsl(${h % 360} 45% 42%)`; }
function cellBg(v, max){ const t = Math.max(0, Math.min(1, v / max)); return t < 0.02 ? '' : `background:color-mix(in srgb, #E9C46A ${Math.round(8 + t * 52)}%, transparent)`; }

function renderProcs(d){
  const all = d.procs, q = LV.q.trim().toLowerCase();
  let list = all.filter(g => (LV.filter === 'all' || (LV.filter === 'windows' ? g.kind === 'windows' : LV.filter === 'app' ? g.kind === 'app' : g.kind === 'background' || g.kind === 'self')) &&
    (!q || (g.name + ' ' + g.desc).toLowerCase().includes(q)));
  const key = {cpu: g => g.cpu, mem: g => g.mem, io: g => g.io, gpu: g => g.gpu}[LV.sort];
  // re-sort every 3 seconds so rows don't jump around constantly
  const now = Date.now();
  if (now - LV.orderAt > 3000 || LV.orderSort !== LV.sort) {
    LV.order = all.slice().sort((a, b) => key(b) - key(a) || b.mem - a.mem).map(g => g.key);
    LV.orderAt = now; LV.orderSort = LV.sort;
  }
  const pos = new Map(LV.order.map((k, i) => [k, i]));
  list.sort((a, b) => (pos.has(a.key) ? pos.get(a.key) : 1e9) - (pos.has(b.key) ? pos.get(b.key) : 1e9));
  const total = list.length, shown = LV.showAll || q ? list : list.slice(0, 15);
  const memMax = d.mem.total || 1;
  const th = (k, label) => `<th data-sort="${k}" class="${LV.sort === k ? 'on' : ''}" scope="col">${label}</th>`;
  $('#procrows').innerHTML = `<table class="ptable"><thead><tr><th scope="col">App or process</th>${th('cpu', 'Processor')}${th('mem', 'Memory')}${th('io', 'Disk and network')}${th('gpu', 'Graphics')}</tr></thead><tbody>
    ${shown.map(g => `<tr><td><div class="app"><div class="mono" style="background:${g.kind === 'self' ? 'var(--pcb)' : monoColor(g.name)}">${esc((g.name[0] || '?').toUpperCase())}</div>
      <div><div class="nm">${esc(g.name)}${g.count > 1 ? `<small>${g.count} processes</small>` : ''}<span class="kind ${g.kind}">${KINDS[g.kind] || g.kind}</span></div>
      <div class="nt">${esc(g.note || g.desc)}</div></div></div></td>
      <td><span class="cell" style="padding:3px 7px;${cellBg(g.cpu, 50)}">${g.cpu.toFixed(1)}%</span></td>
      <td><span class="cell" style="padding:3px 7px;${cellBg(g.mem, memMax * 0.25)}">${memSize(g.mem)}</span></td>
      <td><span class="cell" style="padding:3px 7px;${cellBg(g.io, 2e7)}">${g.io > 0 ? rate(g.io) : '0 KB/s'}</span></td>
      <td><span class="cell" style="padding:3px 7px;${cellBg(g.gpu, 60)}">${g.gpu > 0 ? g.gpu.toFixed(1) + '%' : '0%'}</span></td></tr>`).join('') || '<tr><td colspan="5" class="lead" style="padding:18px 4px">Nothing matches.</td></tr>'}
    </tbody></table>${!LV.showAll && !q && total > 15 ? `<div class="more"><button class="btn" data-lv="all">Show all ${total}</button></div>` : ''}`;
  $('#pmeta').textContent = `${d.nProcs.toLocaleString()} processes, ${d.threads.toLocaleString()} threads, Windows up ${up(d.uptime)}`;
}

/* ---------- the view ---------- */
function liveHTML(){
  return `<div class="board"><svg id="boardsvg" viewBox="0 0 ${W} ${H}" role="img" aria-label="Live view of this PC's processor, memory, storage, graphics and network"></svg>
      <div class="boardnote" id="boardnote"></div></div>
    <div class="lgrid"><div class="lcard" id="heat"></div><div class="lcard" id="activity"></div></div>
    <div class="lcard procs"><h2>What's using your PC</h2><p class="lead">Programs grouped like Task Manager does. Rigscope only watches: it never closes or changes anything.</p>
      <div class="ptools"><div class="seg" role="group" aria-label="Show">
        <button data-lvf="all" class="on">All</button><button data-lvf="app">Apps</button><button data-lvf="background">Background</button><button data-lvf="windows">Windows</button></div>
        <input class="search" id="pq" placeholder="Search" aria-label="Search apps and processes"><span class="pmeta" id="pmeta"></span></div>
      <div id="procrows"></div></div>`;
}

function onLive(d){
  LV.d = d;
  LV.hist.push(d); if (LV.hist.length > HIST) LV.hist.shift();
  if (!$('#boardsvg')) return;
  updateBoard(d);
  renderHeat(d); renderActivity(d); renderProcs(d);
  const note = $('#boardnote');
  if (note) note.innerHTML = d.warnings.includes('throttled') ? `The processor is being slowed down to stay cool or save power.` : '';
  // the specs view also shows a live load bar
  if ($('#cpuload')) { $('#cpuload').style.width = d.cpu.total + '%'; $('#cpuloadv').textContent = Math.round(d.cpu.total) + '%'; }
  if ($('#memuse') && d.mem.total) $('#memuse').textContent = `${gb(d.mem.used, true)} (${Math.round(d.mem.used / d.mem.total * 100)}%)`;
  const el = $('#battlive'), p = d.power;
  if (el && p && p.hasBattery && p.percent >= 0) el.textContent = `${p.acOnline ? (p.charging ? 'Charging' : 'Plugged in') : 'On battery'}, ${p.percent}% now.`;
}

let liveTimer;
async function livePoll(){
  clearTimeout(liveTimer);
  try { onLive(await api('/api/live')); } catch {}
  liveTimer = setTimeout(livePoll, document.hidden ? 4000 : 1000);
}

function liveInit(){
  $('#live').innerHTML = liveHTML();
  if (!LV.raf) LV.raf = requestAnimationFrame(frame);
  livePoll();
}

document.addEventListener('click', e => {
  const s = e.target.closest('[data-sort]'); if (s) { LV.sort = s.dataset.sort; LV.orderAt = 0; LV.d && renderProcs(LV.d); return; }
  const f = e.target.closest('[data-lvf]'); if (f) { LV.filter = f.dataset.lvf; document.querySelectorAll('[data-lvf]').forEach(b => b.classList.toggle('on', b === f)); LV.d && renderProcs(LV.d); return; }
  const a = e.target.closest('[data-lv]'); if (a && a.dataset.lv === 'all') { LV.showAll = true; LV.d && renderProcs(LV.d); }
});
document.addEventListener('input', e => { if (e.target.id === 'pq') { LV.q = e.target.value; LV.d && renderProcs(LV.d); } });

// start: restore the last view the person used, then begin polling
(function(){
  let v = 'live';
  try { v = localStorage.getItem('rigscope-view') || 'live'; } catch {}
  liveInit();
  setView(v === 'specs' ? 'specs' : 'live');
})();
