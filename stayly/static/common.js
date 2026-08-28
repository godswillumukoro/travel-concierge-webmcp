// Shared autocomplete + image loader + carousel (used by the provider apps).

// ---- location/airport autocomplete ----
function attachAutocomplete(inputId, listId, endpoint) {
  const input = document.getElementById(inputId);
  const list = document.getElementById(listId);
  let t = null;
  input.addEventListener('input', () => {
    clearTimeout(t);
    const q = input.value.trim();
    if (q.length < 2) { list.style.display = 'none'; return; }
    t = setTimeout(async () => {
      try {
        const r = await fetch(endpoint + encodeURIComponent(q));
        const d = await r.json();
        list.innerHTML = '';
        const items = d.places || d.airports || [];
        if (!items.length) { list.style.display = 'none'; return; }
        for (const p of items) {
          const el = document.createElement('div');
          el.className = 'ac-item';
          el.innerHTML = `<span>${p.name || p.code || ''}</span><span class="sub">${p.address || p.city || p.country || ''}</span>`;
          el.onmousedown = (e) => { e.preventDefault(); input.value = p.name || p.code; list.style.display = 'none'; };
          list.appendChild(el);
        }
        list.style.display = 'block';
      } catch (e) { list.style.display = 'none'; }
    }, 250);
  });
  document.addEventListener('click', (e) => { if (!e.target.closest('.ac')) list.style.display = 'none'; });
}

// ---- result images + carousel ----
const imgStore = {};
async function loadCardImages() {
  document.querySelectorAll('.card[data-q]').forEach(async (card) => {
    const el = card.querySelector('.card-img');
    if (!el || el.dataset.loaded) return;
    el.dataset.loaded = '1';
    el.textContent = '…';
    const q = card.dataset.q;
    try {
      const r = await fetch('/api/images?q=' + encodeURIComponent(q) + '&limit=6');
      const d = await r.json();
      const imgs = d.images || [];
      if (imgs.length) {
        imgStore[q] = imgs.map(i => i.full);
        el.style.backgroundImage = 'url("' + imgs[0].thumb + '")';
        el.textContent = '';
        el.onclick = () => openCarousel(q);
      } else {
        el.textContent = 'no photo';
      }
    } catch (e) { el.textContent = 'no photo'; }
  });
}
function openCarousel(q) {
  const imgs = imgStore[q] || [];
  if (!imgs.length) return;
  const lb = document.getElementById('lightbox');
  const img = lb.querySelector('img');
  const count = lb.querySelector('.lb-count');
  let i = 0;
  const show = () => { img.src = imgs[i]; count.textContent = (i + 1) + ' / ' + imgs.length; };
  show();
  lb.classList.add('open');
  lb.querySelector('.lb-next').onclick = (e) => { e.stopPropagation(); i = (i + 1) % imgs.length; show(); };
  lb.querySelector('.lb-prev').onclick = (e) => { e.stopPropagation(); i = (i - 1 + imgs.length) % imgs.length; show(); };
  lb.querySelector('.lb-close').onclick = (e) => { e.stopPropagation(); lb.classList.remove('open'); };
  lb.onclick = (e) => { if (e.target === lb) lb.classList.remove('open'); };
}
document.addEventListener('htmx:afterSwap', loadCardImages);
