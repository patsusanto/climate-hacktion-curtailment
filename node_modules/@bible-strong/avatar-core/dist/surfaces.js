//#region src/surfaces.ts
var e = {
	sphere: {
		type: "sphere",
		width: 240,
		height: 240,
		depth: 240,
		roundness: 1
	},
	mickey: {
		type: "mickey",
		width: 220,
		height: 210,
		depth: 145,
		roundness: 1
	},
	cursor: {
		type: "cursor",
		width: 175,
		height: 260,
		depth: 145,
		roundness: 0
	},
	cube: {
		type: "cube",
		width: 245,
		height: 245,
		depth: 220,
		roundness: 0
	},
	capsule: {
		type: "capsule",
		width: 205,
		height: 270,
		depth: 205,
		roundness: 1
	},
	cylinder: {
		type: "cylinder",
		width: 235,
		height: 250,
		depth: 215,
		roundness: .45,
		morphRoundness: 0
	},
	cone: {
		type: "cone",
		width: 250,
		height: 265,
		depth: 225,
		roundness: 0,
		morphRoundness: 0,
		tipRoundness: .55,
		baseRoundness: .45
	},
	diamond: {
		type: "diamond",
		width: 235,
		height: 260,
		depth: 215,
		roundness: 0
	}
}, t = {
	sphere: "Sphère",
	mickey: "Mickey",
	cursor: "Curseur",
	cube: "Cube",
	capsule: "Capsule",
	cylinder: "Cylindre",
	cone: "Cône",
	diamond: "Diamant"
}, n = (e, t) => Math.sign(e) * Math.abs(e) ** t, r = (e, t, r, i, a, o, s) => {
	let c = n(Math.cos(t), o);
	return [
		r / 2 * c * n(Math.sin(e), s),
		i / 2 * n(Math.sin(t), o),
		a / 2 * c * n(Math.cos(e), s)
	];
}, i = (e, t, n) => {
	let r = e.width / 2, i = e.depth / 2, a = Math.min(r, e.height / 2), o = Math.max(0, (e.height - a * 2) / 2), s = o * 2 + Math.PI * a, c = (n + Math.PI / 2) / Math.PI * s, l = r, u = 0;
	if (c < Math.PI * a / 2) {
		let e = -Math.PI / 2 + c / a;
		l = r * Math.cos(e), u = -o + a * Math.sin(e);
	} else if (c <= Math.PI * a / 2 + o * 2) u = -o + c - Math.PI * a / 2;
	else {
		let e = (c - Math.PI * a / 2 - o * 2) / a;
		l = r * Math.cos(e), u = o + a * Math.sin(e);
	}
	let d = r ? i / r : 1;
	return [
		l * Math.sin(t),
		u,
		l * d * Math.cos(t)
	];
}, a = (e) => Math.max(0, Math.min(2, e ?? 0)), o = (e) => 1 + a(e.roundness) / 2, s = .04, c = (e) => e.roundness <= 0 ? Infinity : 2 / (s + a(e.roundness) / 2 * .96), l = (e, t, n, r) => {
	let i = Math.cos(n) * Math.sin(t), a = Math.sin(n), o = Math.cos(n) * Math.cos(t), s = Number.isFinite(r) ? (Math.abs(i) ** r + Math.abs(a) ** r + Math.abs(o) ** r) ** (1 / r) || 1 : Math.max(Math.abs(i), Math.abs(a), Math.abs(o)) || 1;
	return [
		e.width / 2 * (i / s),
		e.height / 2 * (a / s),
		e.depth / 2 * (o / s)
	];
}, u = (e, t, n) => l(e, t, n, o(e)), d = (e, t, n) => l(e, t, n, c(e)), f = .24, p = .2, m = .22, h = (e) => a(e.morphRoundness) / 2, g = (e, t, n) => {
	let r = h(e), i = Math.max(0, Math.min(1, t)), a = Math.sin(i * Math.PI), o = (1 - Math.cos(i * Math.PI)) / 2;
	return {
		radiusScale: n.radiusScale + (a - n.radiusScale) * r,
		verticalProgress: n.verticalProgress + (o - n.verticalProgress) * r
	};
}, _ = (e, t, n, r, i) => {
	let a = 1 - i;
	return a ** 3 * e + 3 * a * a * i * t + 3 * a * i * i * n + i ** 3 * r;
}, v = (e) => ({
	tipFraction: (e.tipRoundness ?? 0) * f,
	baseFraction: (e.baseRoundness ?? 0) * p
}), y = (e, t) => {
	let n = Math.max(0, Math.min(1, t)), r = e.roundness * m;
	if (r <= 0) return {
		radiusScale: 1,
		verticalProgress: (Math.sin((n - .5) * Math.PI) + 1) / 2
	};
	if (n < r) {
		let e = -Math.PI / 2 + n / r * (Math.PI / 2);
		return {
			radiusScale: 1 - r + r * Math.cos(e),
			verticalProgress: (r + r * Math.sin(e)) / 2
		};
	}
	if (n > 1 - r) {
		let e = (n - (1 - r)) / r * (Math.PI / 2);
		return {
			radiusScale: 1 - r + r * Math.cos(e),
			verticalProgress: 1 - r / 2 + r * Math.sin(e) / 2
		};
	}
	let i = (n - r) / (1 - r * 2);
	return {
		radiusScale: 1,
		verticalProgress: r / 2 + i * (1 - r)
	};
}, b = (e, t) => g(e, t, y(e, t)), x = (e, t, n) => {
	let r = Math.max(0, Math.min(1, t)), i = 0, a = 1;
	for (let t = 0; t < 14; t += 1) {
		let t = (i + a) / 2;
		n(e, t).verticalProgress < r ? i = t : a = t;
	}
	return n(e, (i + a) / 2).radiusScale;
}, S = (e, t) => {
	let n = Math.max(0, Math.min(1, t)), { tipFraction: r, baseFraction: i } = v(e);
	if (i > 0 && n < i) {
		let e = n / i;
		return {
			radiusScale: _(1 - i, 1, 1 - i / 2, 1 - i, e),
			verticalProgress: _(0, 0, i / 2, i, e)
		};
	}
	if (r > 0 && n > 1 - r) {
		let e = (n - (1 - r)) / r;
		return {
			radiusScale: _(r, r / 2, r / 4, 0, e),
			verticalProgress: _(1 - r, 1 - r / 2, 1, 1, e)
		};
	}
	return {
		radiusScale: 1 - n,
		verticalProgress: n
	};
}, C = (e, t) => g(e, t, S(e, t)), w = (e) => {
	let t = e.height * .36, n = e.height - t;
	return {
		coneApexY: -e.height / 2,
		coneBaseY: -e.height / 2 + t,
		bodyHeight: n,
		bodyCenterY: e.height / 2 - n / 2,
		bodyWidth: e.width * .54,
		bodyDepth: e.depth * .62
	};
}, T = (e, t, n) => {
	let { width: a, height: o, depth: s } = e;
	switch (e.type) {
		case "sphere":
		case "mickey": return r(t, n, a, o, s, 1, 1);
		case "cube": return d(e, t, n);
		case "cylinder": {
			let r = b(e, (n + Math.PI / 2) / Math.PI);
			return [
				a / 2 * r.radiusScale * Math.sin(t),
				-o / 2 + o * r.verticalProgress,
				s / 2 * r.radiusScale * Math.cos(t)
			];
		}
		case "cursor": {
			let r = w(e), i = (n + Math.PI / 2) / Math.PI, a = y({
				...e,
				width: r.bodyWidth,
				height: r.bodyHeight,
				depth: r.bodyDepth
			}, i);
			return [
				r.bodyWidth / 2 * a.radiusScale * Math.sin(t),
				r.bodyCenterY - r.bodyHeight / 2 + r.bodyHeight * a.verticalProgress,
				r.bodyDepth / 2 * a.radiusScale * Math.cos(t)
			];
		}
		case "diamond": return u(e, t, n);
		case "capsule": return i(e, t, n);
		case "cone": {
			let r = C(e, (n + Math.PI / 2) / Math.PI);
			return [
				a / 2 * r.radiusScale * Math.sin(t),
				o / 2 - o * r.verticalProgress,
				s / 2 * r.radiusScale * Math.cos(t)
			];
		}
	}
}, E = (e, t) => [
	e[0] - t[0],
	e[1] - t[1],
	e[2] - t[2]
], D = ([e, t, n]) => {
	let r = Math.hypot(e, t, n) || 1;
	return [
		e / r,
		t / r,
		n / r
	];
}, O = (e, t, n) => {
	let r = e.type === "cone" ? -1 : 1;
	return D([
		r * (t[1] * n[2] - t[2] * n[1]),
		r * (t[2] * n[0] - t[0] * n[2]),
		r * (t[0] * n[1] - t[1] * n[0])
	]);
}, k = (e, t, n) => {
	let r = 5e-4;
	if (e.type === "cone" && n >= Math.PI / 2 - r) return [
		0,
		-1,
		0
	];
	let i = T(e, t - r, n), a = T(e, t + r, n), o = T(e, t, Math.max(-Math.PI / 2, n - r)), s = T(e, t, Math.min(Math.PI / 2, n + r));
	return O(e, E(a, i), E(s, o));
}, A = (e, t) => Math.sign(e) * Math.abs(e) ** t, j = (e, t, n) => {
	let r = e.width / 2 || 1, i = e.height / 2 || 1, a = e.depth / 2 || 1;
	return D([
		A(t[0] / r, n - 1) / r,
		A(t[1] / i, n - 1) / i,
		A(t[2] / a, n - 1) / a
	]);
}, M = (e, t) => j(e, t, o(e)), N = (e, t) => {
	let n = c(e);
	if (Number.isFinite(n)) return j(e, t, n);
	let r = [
		t[0] / (e.width / 2 || 1),
		t[1] / (e.height / 2 || 1),
		t[2] / (e.depth / 2 || 1)
	], i = r.reduce((e, t, n) => Math.abs(t) > Math.abs(r[e]) ? n : e, 0);
	return [
		i === 0 ? Math.sign(r[0]) : 0,
		i === 1 ? Math.sign(r[1]) : 0,
		i === 2 ? Math.sign(r[2]) : 0
	];
}, P = (e, t, n, r, i) => {
	let a = e.width / 2 || 1, o = e.height / 2 || 1, s = e.depth / 2 || 1;
	if (!Number.isFinite(r)) {
		let r = [
			Math.max(-a, Math.min(a, t)),
			Math.max(-o, Math.min(o, n)),
			s
		];
		return {
			point: r,
			normal: i(e, r)
		};
	}
	let c = Math.max(-1, Math.min(1, n / o)), l = Math.max(0, 1 - Math.abs(c) ** r) ** (1 / r), u = Math.max(-a * l, Math.min(a * l, t)), d = u / a, f = Math.max(0, 1 - Math.abs(d) ** r - Math.abs(c) ** r) ** (1 / r), p = [
		u,
		c * o,
		s * f
	];
	return {
		point: p,
		normal: i(e, p)
	};
}, F = (e, t, n, r, i, a = 0) => {
	let o = t - a, s = Math.max(0, 1 - (e / (n || 1)) ** 2 - (o / (r || 1)) ** 2), c = i * Math.sqrt(s);
	return {
		point: [
			e,
			t,
			c
		],
		normal: D([
			e / (n * n || 1),
			o / (r * r || 1),
			c / (i * i || 1)
		])
	};
}, I = (e, t, n, r, i) => {
	let a = e.width / 2 || 1, o = e.depth / 2 || 1, s = Math.max(0, Math.min(1, .5 + i * (n / e.height))), c = x(e, s, r), l = a * c, u = o * c, d = Math.max(-l, Math.min(l, t)), f = l > 0 ? Math.max(0, 1 - (d / l) ** 2) : 0, p = u * Math.sqrt(f), m = 1e-4, h = Math.max(0, s - m), g = Math.min(1, s + m), _ = x(e, h, r), v = (x(e, g, r) - _) / (g - h || 1), y = Math.max(Math.sqrt(f), 1e-4), b = -(o / a) * d / (l * y || 1), S = i * o * v / (e.height * y || 1);
	return {
		point: [
			d,
			n,
			p
		],
		normal: D([
			-b,
			-S,
			1
		])
	};
}, L = (e, t, n) => {
	let r = e.width / 2 || 1, i = e.height / 2 || 1, a = e.depth / 2 || 1;
	switch (e.type) {
		case "sphere":
		case "mickey": return F(t, n, r, i, a);
		case "cube": return P(e, t, n, c(e), N);
		case "capsule": {
			let e = Math.min(r, i), o = Math.max(0, i - e);
			return F(t, n, r, e, a, n < -o ? -o : n > o ? o : n);
		}
		case "cylinder": return I(e, t, n, b, 1);
		case "cursor": {
			let r = w(e), i = I({
				...e,
				width: r.bodyWidth,
				height: r.bodyHeight,
				depth: r.bodyDepth
			}, t, n - r.bodyCenterY, y, 1);
			return {
				point: [
					i.point[0],
					i.point[1] + r.bodyCenterY,
					i.point[2]
				],
				normal: i.normal
			};
		}
		case "cone": return I(e, t, n, C, -1);
		case "diamond": return P(e, t, n, o(e), M);
	}
}, R = (e, t, n) => {
	let r = T(e, t, n);
	if (e.type === "sphere" || e.type === "mickey") {
		let t = e.width / 2 || 1, n = e.height / 2 || 1, i = e.depth / 2 || 1;
		return D([
			r[0] / (t * t),
			r[1] / (n * n),
			r[2] / (i * i)
		]);
	}
	return e.type === "cylinder" && e.roundness <= 0 && (e.morphRoundness ?? 0) <= 0 ? D([
		Math.sin(t) / (e.width / 2 || 1),
		0,
		Math.cos(t) / (e.depth / 2 || 1)
	]) : e.type === "diamond" ? M(e, r) : e.type === "cube" ? N(e, r) : k(e, t, n);
}, z = (e, t, n) => {
	let r = T(e, t, n);
	if (e.type === "sphere" || e.type === "mickey") {
		let t = e.width / 2 || 1, n = e.height / 2 || 1, i = e.depth / 2 || 1;
		return {
			point: r,
			normal: D([
				r[0] / (t * t),
				r[1] / (n * n),
				r[2] / (i * i)
			])
		};
	}
	return e.type === "cylinder" && e.roundness <= 0 && (e.morphRoundness ?? 0) <= 0 ? {
		point: r,
		normal: D([
			Math.sin(t) / (e.width / 2 || 1),
			0,
			Math.cos(t) / (e.depth / 2 || 1)
		])
	} : e.type === "diamond" ? {
		point: r,
		normal: M(e, r)
	} : e.type === "cube" ? {
		point: r,
		normal: N(e, r)
	} : {
		point: r,
		normal: k(e, t, n)
	};
};
//#endregion
export { w as cursorLayout, L as surfaceFrontSampleAt, t as surfaceLabels, R as surfaceNormalAt, T as surfacePointAt, e as surfacePresets, z as surfaceSampleAt };

//# sourceMappingURL=surfaces.js.map