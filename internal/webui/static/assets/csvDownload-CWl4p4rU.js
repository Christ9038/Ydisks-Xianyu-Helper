import{d as s}from"./download-BaMdvJtq.js";const c=e=>{var l;const o=((l=e.files)==null?void 0:l[0])||null;return e.value="",o},t=e=>e.map(o=>o.map(l=>`"${String(l).replace(/"/g,'""')}"`).join(",")).join(`
`),a=(e,o)=>{s(new Blob(["\uFEFF"+t(o)],{type:"text/csv;charset=utf-8"}),e)};export{c,a as d};
