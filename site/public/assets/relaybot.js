// Relay Bot — reusable mascot SVG for Tekton Events Relay
// modes: 'full' (antenna + head), used everywhere.
window.RelayBot = function (opts) {
  opts = opts || {};
  var w = opts.width || 240;
  var cls = opts.class || '';
  var idp = opts.idPrefix || ('rb' + Math.floor(Math.random() * 1e6)); // unique gradient ids
  return `
<svg class="${cls}" width="${w}" height="${w}" viewBox="0 0 240 240" fill="none" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="Relay Bot">
  <defs>
    <linearGradient id="${idp}-head" x1="0.12" y1="0" x2="0.85" y2="1">
      <stop offset="0" stop-color="#3DEAD6"/>
      <stop offset="0.5" stop-color="#16C4D8"/>
      <stop offset="1" stop-color="#0A98C9"/>
    </linearGradient>
    <linearGradient id="${idp}-edge" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="#0A98C9"/>
      <stop offset="1" stop-color="#0673A6"/>
    </linearGradient>
    <radialGradient id="${idp}-orb" cx="0.5" cy="0.4" r="0.7">
      <stop offset="0" stop-color="#FFE08A"/>
      <stop offset="0.55" stop-color="#FF9A4D"/>
      <stop offset="1" stop-color="#FF6B43"/>
    </radialGradient>
    <linearGradient id="${idp}-eye" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="#CFFEFF"/>
      <stop offset="1" stop-color="#46E2EA"/>
    </linearGradient>
  </defs>

  <!-- antenna -->
  <rect x="116.5" y="36" width="7" height="26" rx="3.5" fill="#0C2A3A"/>
  <circle cx="120" cy="28" r="11" fill="url(#${idp}-orb)"/>
  <circle cx="120" cy="28" r="11" fill="none" stroke="#FFD27A" stroke-opacity="0.5" stroke-width="2"/>

  <!-- head edge + face -->
  <rect x="56" y="60" width="128" height="114" rx="36" fill="url(#${idp}-edge)"/>
  <rect x="56" y="56" width="128" height="112" rx="36" fill="url(#${idp}-head)"/>
  <rect x="68" y="66" width="104" height="30" rx="15" fill="#FFFFFF" fill-opacity="0.16"/>

  <!-- side ports -->
  <circle cx="56" cy="120" r="11" fill="url(#${idp}-edge)"/>
  <circle cx="56" cy="120" r="5" fill="#0C2A3A"/>
  <circle cx="184" cy="120" r="11" fill="url(#${idp}-edge)"/>
  <circle cx="184" cy="120" r="5" fill="#0C2A3A"/>

  <!-- face screen -->
  <rect x="76" y="88" width="88" height="60" rx="22" fill="#08243A"/>
  <rect x="76" y="88" width="88" height="60" rx="22" fill="none" stroke="#0B3553" stroke-width="2"/>

  <!-- eyes -->
  <rect x="96" y="104" width="13" height="22" rx="6.5" fill="url(#${idp}-eye)"/>
  <rect x="131" y="104" width="13" height="22" rx="6.5" fill="url(#${idp}-eye)"/>
  <circle cx="102.5" cy="109" r="2.6" fill="#FFFFFF"/>
  <circle cx="137.5" cy="109" r="2.6" fill="#FFFFFF"/>

  <!-- mouth: relay equalizer -->
  <rect x="108" y="135" width="5" height="7" rx="2.5" fill="#46E2EA"/>
  <rect x="117.5" y="131" width="5" height="11" rx="2.5" fill="#7CEBEF"/>
  <rect x="127" y="135" width="5" height="7" rx="2.5" fill="#46E2EA"/>
</svg>`;
};

// Fan-out network overlay for the banner (1280x400 coordinate space)
window.RelayNet = function () {
  var nodes = [
    {x:1150,y:92,c:'#46E2EA'},
    {x:1150,y:164,c:'#3E8BFF'},
    {x:1150,y:236,c:'#19C7A6'},
    {x:1150,y:308,c:'#FF7A59'}
  ];
  var px=250, py=198, lines='', dots='';
  nodes.forEach(function(n){
    var cx=(px+n.x)/2;
    lines+=`<path d="M ${px} ${py} C ${cx} ${py}, ${cx} ${n.y}, ${n.x} ${n.y}" stroke="${n.c}" stroke-width="2.5" stroke-opacity="0.34" fill="none"/>`;
    dots+=`<circle cx="${n.x-150}" cy="${n.y}" r="3.5" fill="${n.c}" fill-opacity="0.6"/>`;
    dots+=`<g><rect x="${n.x-15}" y="${n.y-15}" width="30" height="30" rx="9" fill="${n.c}" fill-opacity="0.16"/>`
        + `<circle cx="${n.x}" cy="${n.y}" r="8" fill="${n.c}"/>`
        + `<circle cx="${n.x}" cy="${n.y}" r="8" fill="none" stroke="${n.c}" stroke-opacity="0.4" stroke-width="6"/></g>`;
  });
  return `<svg class="net" viewBox="0 0 1280 400" fill="none" xmlns="http://www.w3.org/2000/svg">${lines}${dots}</svg>`;
};

// Full banner inner markup (expects .bot/.txt/.net CSS on a 1280x400 parent)
window.RelayBannerInner = function () {
  return window.RelayNet()
    + `<div class="bot">${window.RelayBot({width:208, idPrefix:'bn'+Math.floor(Math.random()*1e5)})}</div>`
    + `<div class="txt">`
    +   `<p class="kick">CloudEvents Bridge</p>`
    +   `<h1>Tekton Events<br><span class="accent">Relay</span></h1>`
    +   `<p>Automate pipeline feedback to GitHub, GitLab, Slack &amp; more.</p>`
    + `</div>`;
};
