import { surfaceLabels as e, surfacePresets as t } from "./surfaces.js";
//#region src/body.ts
var n = [
	"sphere",
	"cube",
	"capsule",
	"cylinder",
	"cone",
	"diamond"
], r = 16, i = Object.keys(t), a = (e) => typeof e == "number" && Number.isFinite(e), o = (e) => Array.isArray(e) && e.length === 3 && e.every(a), s = (e, n) => {
	if (!e || typeof e != "object") return { ...n };
	let r = e, o = r.type && i.includes(r.type) ? r.type : n.type, s = t[o];
	return [
		"width",
		"height",
		"depth",
		"roundness"
	].some((e) => !a(r[e])) || r.morphRoundness !== void 0 && !a(r.morphRoundness) || r.tipRoundness !== void 0 && !a(r.tipRoundness) || r.baseRoundness !== void 0 && !a(r.baseRoundness) ? { ...n } : {
		...s,
		...r,
		type: o
	};
}, c = (e, r) => {
	if (!e || typeof e != "object") return {
		primary: r,
		nodes: []
	};
	let i = e, c = s(i.primary, r), l = /* @__PURE__ */ new Set();
	return {
		primary: c,
		nodes: Array.isArray(i.nodes) ? i.nodes.filter((e) => {
			if (!e || typeof e != "object") return !1;
			let t = e.surface, r = e.id;
			if (r === "primary" || l.has(r)) return !1;
			let i = !!(typeof e.id == "string" && r && typeof e.name == "string" && t && n.includes(t.type) && a(t.width) && a(t.height) && a(t.depth) && a(t.roundness) && o(e.position) && o(e.rotation));
			return i && l.add(r), i;
		}).slice(0, 16).map((e) => ({
			...e,
			surface: s(e.surface, t[e.surface.type])
		})) : []
	};
}, l = (n, r) => {
	let i = t[n], a = .34, o = r % 2 == 0 ? -1 : 1;
	return {
		id: `shape-${crypto.randomUUID()}`,
		name: `${e[n]} ${r + 1}`,
		surface: {
			...i,
			width: i.width * a,
			height: i.height * a,
			depth: i.depth * a
		},
		position: [
			o * 82,
			-72,
			-18
		],
		rotation: [
			0,
			0,
			0
		]
	};
}, u = (e) => ({
	...e,
	id: `shape-${crypto.randomUUID()}`,
	name: `${e.name} copie`,
	surface: { ...e.surface },
	position: [
		e.position[0] + 14,
		e.position[1] + 14,
		e.position[2]
	],
	rotation: [...e.rotation]
});
//#endregion
export { r as MAX_BODY_NODES, n as bodyPrimitiveTypes, l as createBodyNode, u as duplicateBodyNode, c as parseAvatarBody, s as parseSurfaceConfig };

//# sourceMappingURL=body.js.map