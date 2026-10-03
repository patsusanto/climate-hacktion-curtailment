import { cursorLayout as e, surfaceFrontSampleAt as t, surfacePointAt as n, surfaceSampleAt as r } from "./surfaces.js";
//#region src/geometry.ts
var i = 120, a = 620, o = 14, s = [
	"headX",
	"headY",
	"headZ",
	"widthLeft",
	"widthRight",
	"heightLeft",
	"heightRight",
	"spacing",
	"positionXLeft",
	"positionXRight",
	"positionYLeft",
	"positionYRight",
	"leftAngle",
	"rightAngle",
	"perspective"
], c = (e, t, n) => Math.max(t, Math.min(n, e)), l = (e) => e * Math.PI / 180, u = ([e, t, n, r]) => {
	let i = Math.hypot(e, t, n, r) || 1;
	return [
		e / i,
		t / i,
		n / i,
		r / i
	];
}, d = ([e, t, n, r], [i, a, o, s]) => u([
	e * i - t * a - n * o - r * s,
	e * a + t * i + n * s - r * o,
	e * o - t * s + n * i + r * a,
	e * s + t * o - n * a + r * i
]), f = ([e, t, n], r) => {
	let i = r / 2, a = Math.sin(i);
	return u([
		Math.cos(i),
		e * a,
		t * a,
		n * a
	]);
}, p = (e, t, n) => {
	let r = f([
		1,
		0,
		0
	], e), i = f([
		0,
		1,
		0
	], t);
	return d(d(f([
		0,
		0,
		1
	], n), r), i);
}, m = (e, t) => {
	let n = e[0] * t[0] + e[1] * t[1] + e[2] * t[2], r = [
		e[1] * t[2] - e[2] * t[1],
		e[2] * t[0] - e[0] * t[2],
		e[0] * t[1] - e[1] * t[0]
	];
	return u([
		1 + n,
		r[0],
		r[1],
		r[2]
	]);
}, h = ([e, t, n, r]) => {
	let i = 1 - 2 * (n * n + r * r), a = 2 * (t * n - r * e), o = 2 * (t * n + r * e), s = 1 - 2 * (t * t + r * r), l = 2 * (t * r - n * e), u = 2 * (n * r + t * e), d = 1 - 2 * (t * t + n * n), f = Math.asin(c(u, -1, 1));
	return Math.abs(Math.cos(f)) < 1e-5 ? [
		f,
		0,
		Math.atan2(o, i)
	] : [
		f,
		Math.atan2(-l, d),
		Math.atan2(-a, s)
	];
}, g = (e, t) => {
	let n = e;
	for (; n - t > 180;) n -= 360;
	for (; n - t < -180;) n += 360;
	return c(n, -365, 365);
}, _ = (e, t) => {
	let [n, r, i] = h(t), a = n * 180 / Math.PI, o = r * 180 / Math.PI, s = i * 180 / Math.PI;
	return {
		...e,
		headX: g(a, e.headX),
		headY: g(o, e.headY),
		headZ: g(s, e.headZ)
	};
}, v = (e, t, n) => {
	let r = t, i = e.reduce((e, t, n) => e + t * r[n], 0);
	if (i < 0 && (r = r.map((e) => -e), i = -i), i > .9995) return u(e.map((e, t) => e + (r[t] - e) * n));
	let a = Math.acos(c(i, -1, 1)), o = Math.sin(a), s = Math.sin((1 - n) * a) / o, l = Math.sin(n * a) / o;
	return u(e.map((e, t) => e * s + r[t] * l));
}, y = ([e, t, n, r], [i, a, o]) => {
	let s = 2 * (n * o - r * a), c = 2 * (r * i - t * o), l = 2 * (t * a - n * i);
	return [
		i + e * s + (n * l - r * c),
		a + e * c + (r * s - t * l),
		o + e * l + (t * c - n * s)
	];
}, b = (e, t) => {
	let n = e / 2, r = t / 2, i = Math.min(r, n), a = [], s = (e, t) => {
		let n = Math.max(2, Math.ceil(Math.hypot(t[0] - e[0], t[1] - e[1]) / 1.5));
		for (let r = 0; r < n; r += 1) {
			let i = r / n;
			a.push([e[0] + (t[0] - e[0]) * i, e[1] + (t[1] - e[1]) * i]);
		}
	}, c = (e, t, n) => {
		for (let r = 0; r < o; r += 1) {
			let s = n + r / o * (Math.PI / 2);
			a.push([e + Math.cos(s) * i, t + Math.sin(s) * i]);
		}
	};
	return s([-n + i, -r], [n - i, -r]), c(n - i, -r + i, -Math.PI / 2), s([n, -r + i], [n, r - i]), c(n - i, r - i, 0), s([n - i, r], [-n + i, r]), c(-n + i, r - i, Math.PI / 2), s([-n, r - i], [-n, -r + i]), c(-n + i, -r + i, Math.PI), a;
}, x = (e, t) => {
	let n = a - e[2] * t, r = Math.abs(n) < 1e-4 ? a / 1e-4 : a / n;
	return [
		e[0] * r,
		e[1] * r,
		e[2]
	];
}, S = (e) => e === "x" ? [
	1,
	0,
	0
] : e === "y" ? [
	0,
	1,
	0
] : [
	0,
	0,
	1
], C = (e, t, n) => {
	let r = O(e).orientation;
	return _(e, d(f(y(r, S(t)), l(n)), r));
}, w = (e, t) => {
	let n = O(e).orientation;
	return _(e, d(f([
		0,
		0,
		1
	], t), n));
}, T = ([e, t]) => {
	let n = e / 120, r = t / 120, i = n * n + r * r;
	if (i <= 1) return [
		n,
		r,
		Math.sqrt(1 - i)
	];
	let a = Math.sqrt(i);
	return [
		n / a,
		r / a,
		0
	];
}, ee = (e, t, n) => {
	let r = O(e).orientation;
	return _(e, d(m(T(t), T(n)), r));
}, te = (e, t, n = 30) => Array.from({ length: 97 }, (r, i) => {
	let a = i / 96 * Math.PI * 2, o = Math.cos(a), s = Math.sin(a), c = t === "x" ? [
		0,
		o,
		s
	] : t === "y" ? [
		o,
		0,
		s
	] : [
		o,
		s,
		0
	], l = y(e.orientation, c);
	return [
		l[0] * n,
		l[1] * n,
		l[2]
	];
}), ne = (e, t, n = 34, r = 26) => {
	let i = (t) => x(y(e.orientation, t), e.expression.perspective), a = i(t.position), o = p(l(t.rotation[0]), l(t.rotation[1]), l(t.rotation[2])), s = Object.fromEntries([
		"x",
		"y",
		"z"
	].map((e) => {
		let r = y(o, S(e));
		return [e, i([
			t.position[0] + r[0] * n,
			t.position[1] + r[1] * n,
			t.position[2] + r[2] * n
		])];
	}));
	return [
		"x",
		"y",
		"z"
	].forEach((e) => {
		let t = s[e];
		if (Math.hypot(t[0] - a[0], t[1] - a[1]) >= 12) return;
		let n = e === "x" ? [
			a[0] + 18,
			a[1],
			t[2]
		] : e === "y" ? [
			a[0],
			a[1] + 18,
			t[2]
		] : [
			a[0] + 14,
			a[1] + 14,
			t[2]
		];
		s[e] = n;
	}), {
		center: a,
		axes: s,
		rings: Object.fromEntries([
			"x",
			"y",
			"z"
		].map((e) => [e, Array.from({ length: 65 }, (n, a) => {
			let s = a / 64 * Math.PI * 2, c = Math.cos(s) * r, l = Math.sin(s) * r, u = y(o, e === "x" ? [
				0,
				c,
				l
			] : e === "y" ? [
				c,
				0,
				l
			] : [
				c,
				l,
				0
			]);
			return i([
				t.position[0] + u[0],
				t.position[1] + u[1],
				t.position[2] + u[2]
			]);
		})]))
	};
}, re = (e, t, n) => {
	let r = y(p(l(e.rotation[0]), l(e.rotation[1]), l(e.rotation[2])), S(t));
	return {
		...e,
		position: [
			e.position[0] + r[0] * n,
			e.position[1] + r[1] * n,
			e.position[2] + r[2] * n
		]
	};
}, ie = (e, t, n, r) => {
	let i = a - y(t.orientation, e.position)[2] * t.expression.perspective, o = Math.abs(i) < 1e-4 ? a / 1e-4 : a / i, [s, c, l, u] = t.orientation, d = y([
		s,
		-c,
		-l,
		-u
	], [
		n / o,
		r / o,
		0
	]);
	return {
		...e,
		position: [
			e.position[0] + d[0],
			e.position[1] + d[1],
			e.position[2] + d[2]
		]
	};
}, E = (e, t, n) => {
	let r = h(d(p(l(e.rotation[0]), l(e.rotation[1]), l(e.rotation[2])), f(S(t), l(n)))).map((e) => e * 180 / Math.PI);
	return {
		...e,
		rotation: r.map((t, n) => g(t, e.rotation[n]))
	};
}, D = (e, t = !0) => e.length ? `M${e[0][0].toFixed(2)} ${e[0][1].toFixed(2)}${e.slice(1).map((e) => `L${e[0].toFixed(2)} ${e[1].toFixed(2)}`).join("")}${t ? "Z" : ""}` : "", O = (e) => ({
	expression: e,
	orientation: p(l(e.headX), l(e.headY), l(e.headZ))
}), ae = (e, t, n) => {
	let r = { ...e.expression };
	return s.forEach((i) => {
		let a = t.expression[i];
		(i === "headX" || i === "headY" || i === "headZ" || i === "leftAngle" || i === "rightAngle") && (a = g(a, e.expression[i])), r[i] = e.expression[i] + (a - e.expression[i]) * n;
	}), {
		expression: r,
		orientation: O(r).orientation
	};
}, oe = 24, se = 25, ce = 73, k = 144, le = 33, ue = 73, A = /* @__PURE__ */ new Map(), j = /* @__PURE__ */ new Map(), M = /* @__PURE__ */ new Map(), N = (e) => JSON.stringify([
	e.type,
	e.width,
	e.height,
	e.depth,
	e.roundness,
	e.morphRoundness,
	e.tipRoundness,
	e.baseRoundness
]), P = (e, t, n) => (e.size >= oe && e.delete(e.keys().next().value), e.set(t, n), n), F = (e, t, n) => r(e, t, n), I = (e, t) => ({
	point: x(y(e.orientation, t.point), e.expression.perspective),
	normal: y(e.orientation, t.normal)
}), de = (e, t) => {
	let n = e / 120, r = t / 120;
	return [120 * Math.cos(r) * Math.sin(n), 120 * Math.sin(r)];
}, L = (e, n, r, i) => {
	let [a, o] = de(r, i);
	return I(e, t(n, a, o));
}, R = (e, t, n, r, i = {
	x: 0,
	y: 0
}) => {
	let a = e.expression, o = n < 0 ? "Left" : "Right", s = a[`width${o}`], c = 5 + (a[`height${o}`] - 5) * r, u = n * a.spacing / 2 + a[`positionX${o}`] + i.x, d = a[`positionY${o}`] + i.y, f = l(n < 0 ? a.leftAngle : a.rightAngle);
	return b(s, c).map(([n, r]) => {
		let i = n * Math.cos(f) - r * Math.sin(f), a = n * Math.sin(f) + r * Math.cos(f);
		return L(e, t, u + i, d + a);
	});
}, z = (e) => {
	let t = [], n = [];
	return e.forEach(({ point: e, normal: r }) => {
		r[2] > 0 ? n.push(e) : n.length && (t.push(n), n = []);
	}), n.length && t.push(n), t.filter((e) => e.length > 1).map((e) => D(e, !1)).join("");
}, B = (e, t) => {
	let n = N(t), r = M.get(n);
	if (!r) {
		let e = [
			-60,
			-30,
			0,
			30,
			60
		].map((e) => Array.from({ length: 73 }, (n, r) => F(t, l(-180 + r * 5), l(e)))), i = Array.from({ length: 12 }, (e, t) => -150 + t * 30).map((e) => Array.from({ length: 37 }, (n, r) => F(t, l(e), l(-90 + r * 5))));
		r = P(M, n, [...e, ...i]);
	}
	return r.map((t) => z(t.map((t) => I(e, t))));
}, V = (e, t, n, r, i) => {
	let a = e.expression, o = n < 0 ? "Left" : "Right", s = l(n < 0 ? a.leftAngle : a.rightAngle), c = r * Math.cos(s) - i * Math.sin(s), u = r * Math.sin(s) + i * Math.cos(s);
	return L(e, t, n * a.spacing / 2 + a[`positionX${o}`] + c, a[`positionY${o}`] + u).point;
}, H = (e, t, n) => {
	let r = e.expression, i = n < 0 ? "Left" : "Right", a = r[`width${i}`], o = r[`height${i}`], s = R(e, t, n, 1), c = s.map((e) => e.point), l = V(e, t, n, 0, 0), u = V(e, t, n, a / 2 + 9, 0), d = V(e, t, n, 0, -o / 2 - 9), f = V(e, t, n, 0, -o / 2 - 30), p = V(e, t, n, a / 2 + 11, o / 2 + 11), m = V(e, t, -1, 0, 0), h = V(e, t, 1, 0, 0), g = L(e, t, (r.positionXLeft + r.positionXRight) / 2, (r.positionYLeft + r.positionYRight) / 2 + o / 2 + 34).point, _ = [
		(m[0] + h[0]) / 2,
		(m[1] + h[1]) / 2,
		(m[2] + h[2]) / 2
	], v = (e, t) => D([e, t], !1);
	return {
		visible: s.reduce((e, t) => e + t.normal[2], 0) > 0,
		selectionPath: D(c),
		widthGuide: v(l, u),
		heightGuide: v(l, d),
		rotationGuide: v(d, f),
		spacingGuide: `${v(m, h)}${v(_, g)}`,
		center: l,
		widthHandle: u,
		heightHandle: d,
		rotateHandle: f,
		sizeHandle: p,
		spacingHandle: g
	};
}, U = (e) => {
	let t = [...e].sort((e, t) => e[0] - t[0] || e[1] - t[1]), n = (e, t, n) => (t[0] - e[0]) * (n[1] - e[1]) - (t[1] - e[1]) * (n[0] - e[0]), r = (e) => {
		let t = [];
		return e.forEach((e) => {
			for (; t.length >= 2 && n(t.at(-2), t.at(-1), e) <= 0;) t.pop();
			t.push(e);
		}), t;
	};
	return [...r(t).slice(0, -1), ...r(t.reverse()).slice(0, -1)];
}, W = (e) => {
	if (e.length < 3) return D(e);
	let t = (t) => e[(t + e.length) % e.length];
	return `M${e[0][0].toFixed(2)} ${e[0][1].toFixed(2)}${e.map((e, n) => {
		let r = t(n - 1), i = t(n + 1), a = t(n + 2), o = [
			e[0] + (i[0] - r[0]) / 6,
			e[1] + (i[1] - r[1]) / 6,
			e[2]
		], s = [
			i[0] - (a[0] - e[0]) / 6,
			i[1] - (a[1] - e[1]) / 6,
			i[2]
		];
		return `C${o[0].toFixed(2)} ${o[1].toFixed(2)} ${s[0].toFixed(2)} ${s[1].toFixed(2)} ${i[0].toFixed(2)} ${i[1].toFixed(2)}`;
	}).join("")}Z`;
}, G = (e, t = 7) => e.flatMap((n, r) => {
	let i = e[(r + 1) % e.length], a = Math.max(1, Math.ceil(Math.hypot(i[0] - n[0], i[1] - n[1]) / t));
	return Array.from({ length: a }, (e, t) => {
		let r = t / a;
		return [
			n[0] + (i[0] - n[0]) * r,
			n[1] + (i[1] - n[1]) * r,
			n[2] + (i[2] - n[2]) * r
		];
	});
}), fe = (e) => e.length ? e.length === 1 ? `${e[0][0].toFixed(2)} ${e[0][1].toFixed(2)}` : e.slice(0, -1).map((t, n) => {
	let r = e[Math.max(0, n - 1)], i = e[n + 1], a = e[Math.min(e.length - 1, n + 2)], o = t[0] + (i[0] - r[0]) / 6, s = t[1] + (i[1] - r[1]) / 6, c = i[0] - (a[0] - t[0]) / 6, l = i[1] - (a[1] - t[1]) / 6;
	return `C${o.toFixed(2)} ${s.toFixed(2)} ${c.toFixed(2)} ${l.toFixed(2)} ${i[0].toFixed(2)} ${i[1].toFixed(2)}`;
}).join("") : "", K = (e, t) => x(y(e.orientation, t), e.expression.perspective), q = (e, t, n) => Array.from({ length: 145 }, (r, i) => {
	let a = i / k * Math.PI * 2;
	return [
		e / 2 * Math.sin(a),
		n,
		t / 2 * Math.cos(a)
	];
}), J = (e, t) => {
	let r = N(t), i = A.get(r);
	return i || (i = Array.from({ length: le }, (e, r) => {
		let i = -Math.PI / 2 + r / 32 * Math.PI;
		return Array.from({ length: ue }, (e, r) => {
			let a = -Math.PI + r / 72 * Math.PI * 2;
			return n(t, a, i);
		});
	}).flat(), P(A, r, i)), W(G(U(i.map((t) => K(e, t)))));
}, pe = (e, t) => {
	if (t.roundness > 0 || (t.morphRoundness ?? 0) > 0) return J(e, t);
	let n = t.height / 2;
	return W(G(U([...q(t.width, t.depth, -n), ...q(t.width, t.depth, n)].map((t) => K(e, t)))));
}, me = (t, n) => {
	let r = e(n), i = r.bodyHeight / 2;
	return W(G(U([...q(r.bodyWidth, r.bodyDepth, r.bodyCenterY - i), ...q(r.bodyWidth, r.bodyDepth, r.bodyCenterY + i)].map((e) => K(t, e)))));
}, he = (t, n) => {
	let r = e(n), i = K(t, [
		0,
		r.coneApexY,
		0
	]);
	return W(G(U([...q(n.width, n.depth, r.coneBaseY).map((e) => K(t, e)), i])));
}, ge = (e, t) => {
	if ((t.morphRoundness ?? 0) > 0 || (t.tipRoundness ?? 0) > 0 || (t.baseRoundness ?? 0) > 0) return J(e, t);
	let n = K(e, [
		0,
		-t.height / 2,
		0
	]), r = U([...q(t.width, t.depth, t.height / 2).map((t) => K(e, t)), n]), i = r.findIndex((e) => Math.hypot(e[0] - n[0], e[1] - n[1]) < .01);
	if (i < 0) return W(r);
	let a = [...r.slice(i), ...r.slice(0, i)].slice(1);
	return a.length < 2 ? D(r) : `M${n[0].toFixed(2)} ${n[1].toFixed(2)}L${a[0][0].toFixed(2)} ${a[0][1].toFixed(2)}${fe(a)}L${n[0].toFixed(2)} ${n[1].toFixed(2)}Z`;
}, _e = (e, t) => {
	if (t.roundness > 0) return J(e, t);
	let n = t.width / 2, r = t.height / 2, i = t.depth / 2;
	return D(U([-1, 1].flatMap((e) => [-1, 1].flatMap((t) => [-1, 1].map((a) => [
		e * n,
		t * r,
		a * i
	]))).map((t) => K(e, t))));
}, ve = (e, t) => {
	if (t.roundness > 0) return J(e, t);
	let n = t.width / 2, r = t.height / 2, i = t.depth / 2;
	return D(U([
		[
			-n,
			0,
			0
		],
		[
			n,
			0,
			0
		],
		[
			0,
			-r,
			0
		],
		[
			0,
			r,
			0
		],
		[
			0,
			0,
			-i
		],
		[
			0,
			0,
			i
		]
	].map((t) => K(e, t))));
}, Y = (e, t, n, r, i) => {
	let a = n + i, o = Math.hypot(n - i, r * 2), s = (a + o) / 2, c = (a - o) / 2;
	return s <= 0 || c <= 0 ? null : {
		centerX: e,
		centerY: t,
		majorRadius: Math.sqrt(s),
		minorRadius: Math.sqrt(c),
		rotation: Math.atan2(r * 2, n - i) / 2
	};
}, X = ({ centerX: e, centerY: t, majorRadius: n, minorRadius: r, rotation: i }) => {
	let a = i * 180 / Math.PI, o = Math.cos(i) * n, s = Math.sin(i) * n, c = e + o, l = t + s, u = e - o, d = t - s;
	return `M${c.toFixed(2)} ${l.toFixed(2)}A${n.toFixed(2)} ${r.toFixed(2)} ${a.toFixed(2)} 0 1 ${u.toFixed(2)} ${d.toFixed(2)}A${n.toFixed(2)} ${r.toFixed(2)} ${a.toFixed(2)} 0 1 ${c.toFixed(2)} ${l.toFixed(2)}Z`;
}, Z = (e, t, n = [
	0,
	0,
	0
]) => {
	let r = [
		y(e.orientation, [
			1,
			0,
			0
		]),
		y(e.orientation, [
			0,
			1,
			0
		]),
		y(e.orientation, [
			0,
			0,
			1
		])
	], i = y(e.orientation, n);
	if (Math.abs(e.expression.perspective) < 1e-4) {
		let e = r.reduce((e, n, r) => e + n[0] * n[0] * t[r] * t[r], 0), n = r.reduce((e, n, r) => e + n[0] * n[1] * t[r] * t[r], 0), a = r.reduce((e, n, r) => e + n[1] * n[1] * t[r] * t[r], 0);
		return Y(i[0], i[1], e, n, a);
	}
	let o = t.map((e) => 1 / (e * e)), s = Array.from({ length: 3 }, (e, t) => Array.from({ length: 3 }, (e, n) => r.reduce((e, r, i) => e + r[t] * o[i] * r[n], 0))), c = a / e.expression.perspective, l = [
		-i[0],
		-i[1],
		c - i[2]
	], u = [
		s[0][0] * l[0] + s[0][1] * l[1] + s[0][2] * l[2],
		s[1][0] * l[0] + s[1][1] * l[1] + s[1][2] * l[2],
		s[2][0] * l[0] + s[2][1] * l[1] + s[2][2] * l[2]
	], d = l[0] * u[0] + l[1] * u[1] + l[2] * u[2] - 1, f = [
		u[0],
		u[1],
		-c * u[2]
	], p = [
		[
			s[0][0],
			s[0][1],
			-c * s[0][2]
		],
		[
			s[1][0],
			s[1][1],
			-c * s[1][2]
		],
		[
			-c * s[2][0],
			-c * s[2][1],
			c * c * s[2][2]
		]
	], m = Array.from({ length: 3 }, (e, t) => Array.from({ length: 3 }, (e, n) => f[t] * f[n] - d * p[t][n])), h = m[0][0] * m[1][1] - m[0][1] * m[0][1];
	if (Math.abs(h) < 1e-12) return null;
	let g = -(m[1][1] * m[0][2] - m[0][1] * m[1][2]) / h, _ = (m[0][1] * m[0][2] - m[0][0] * m[1][2]) / h, v = -(m[2][2] + m[0][2] * g + m[1][2] * _);
	if (Math.abs(v) < 1e-12) return null;
	let b = m[0][0] / v, x = m[0][1] / v, S = m[1][1] / v, C = b * S - x * x;
	return C <= 0 ? null : Y(g, _, S / C, -x / C, b / C);
}, ye = (e, t) => {
	let n = Z(e, [
		t.width / 2,
		t.height / 2,
		t.depth / 2
	]), r = t.width === t.height && t.height === t.depth;
	if (n && r) {
		let e = (n.majorRadius + n.minorRadius) / 2;
		return X({
			centerX: 0,
			centerY: 0,
			majorRadius: e,
			minorRadius: e,
			rotation: 0
		});
	}
	return n ? X(n) : null;
}, be = (e, t) => {
	if (t.type !== "mickey") return [];
	let n = Math.min(t.width, t.height) * .23, r = Math.min(n, t.depth * .29), i = t.width * .37, a = -t.height * .39, o = -t.depth * .12, s = [
		n,
		n,
		r
	];
	return [-1, 1].map((t) => Z(e, s, [
		t * i,
		a,
		o
	])).filter((e) => e !== null).map(X);
}, xe = (e, t) => t.type === "mickey" ? be(e, t) : t.type === "cursor" ? [he(e, t)] : [], Q = (e) => Array.from({ length: k }, (t, n) => {
	let r = n / k * Math.PI * 2, i = Math.cos(r) * e.majorRadius, a = Math.sin(r) * e.minorRadius;
	return [
		e.centerX + i * Math.cos(e.rotation) - a * Math.sin(e.rotation),
		e.centerY + i * Math.sin(e.rotation) + a * Math.cos(e.rotation),
		0
	];
}), Se = (e) => {
	if (e.length < 3) return D(e);
	let t = e.map((t, n) => {
		let r = e[(n + 1) % e.length];
		return Math.hypot(r[0] - t[0], r[1] - t[1]);
	}), n = [...t].sort((e, t) => e - t), r = n[Math.floor(n.length / 2)] || 1, i = Math.max(8, r * 3.5), a = t.map((e) => e > i);
	return `M${e[0][0].toFixed(2)} ${e[0][1].toFixed(2)}${e.map((t, n) => {
		let r = (n + 1) % e.length, i = e[r];
		if (a[n]) return `L${i[0].toFixed(2)} ${i[1].toFixed(2)}`;
		let o = a[(n - 1 + e.length) % e.length] ? t : e[(n - 1 + e.length) % e.length], s = a[r] ? i : e[(n + 2) % e.length], c = t[0] + (i[0] - o[0]) / 6, l = t[1] + (i[1] - o[1]) / 6, u = i[0] - (s[0] - t[0]) / 6, d = i[1] - (s[1] - t[1]) / 6;
		return `C${c.toFixed(2)} ${l.toFixed(2)} ${u.toFixed(2)} ${d.toFixed(2)} ${i[0].toFixed(2)} ${i[1].toFixed(2)}`;
	}).join("")}Z`;
}, Ce = (e, t) => {
	let n = t.width / 2, r = Math.min(n, t.height / 2), i = t.depth / 2, a = Math.max(0, (t.height - r * 2) / 2), o = [
		n,
		r,
		i
	], s = Z(e, o, [
		0,
		a,
		0
	]), c = Z(e, o, [
		0,
		-a,
		0
	]);
	return !s || !c ? null : Se(U([...Q(s), ...Q(c)]));
}, $ = (e, t) => {
	if (t.type === "sphere" || t.type === "mickey") {
		let n = ye(e, t);
		if (n) return n;
	}
	if (t.type === "capsule") {
		let n = Ce(e, t);
		if (n) return n;
	}
	if (t.type === "cylinder") return pe(e, t);
	if (t.type === "cursor") return me(e, t);
	if (t.type === "cone") return ge(e, t);
	if (t.type === "cube") return _e(e, t);
	if (t.type === "diamond") return ve(e, t);
	let r = N(t), i = A.get(r);
	return i || (i = Array.from({ length: se }, (e, r) => {
		let i = -Math.PI / 2 + r / 24 * Math.PI;
		return Array.from({ length: ce }, (e, r) => {
			let a = -Math.PI + r / 72 * Math.PI * 2;
			return n(t, a, i);
		});
	}).flat(), P(A, r, i)), D(U(i.map((t) => x(y(e.orientation, t), e.expression.perspective))));
}, we = (e, t) => {
	let r = N(t.surface), i = j.get(r);
	i || (i = Array.from({ length: 17 }, (e, r) => {
		let i = -Math.PI / 2 + r / 16 * Math.PI;
		return Array.from({ length: 49 }, (e, r) => {
			let a = -Math.PI + r / 48 * Math.PI * 2;
			return n(t.surface, a, i);
		});
	}).flat(), P(j, r, i));
	let a = p(l(t.rotation[0]), l(t.rotation[1]), l(t.rotation[2])), o = U(i.map((n) => {
		let r = y(a, n), i = [
			r[0] + t.position[0],
			r[1] + t.position[1],
			r[2] + t.position[2]
		];
		return x(y(e.orientation, i), e.expression.perspective);
	}));
	return (t.surface.type === "cube" || t.surface.type === "diamond") && t.surface.roundness <= 0 ? D(o) : W(G(o));
}, Te = .1, Ee = (e, t) => {
	let n = p(l(t.rotation[0]), l(t.rotation[1]), l(t.rotation[2])), r = [
		[
			1,
			0,
			0
		],
		[
			0,
			1,
			0
		],
		[
			0,
			0,
			1
		]
	].map((t) => y(e.orientation, y(n, t))[2]);
	return Math.hypot(r[0] * (t.surface.width / 2), r[1] * (t.surface.height / 2), r[2] * (t.surface.depth / 2));
}, De = (e, t) => {
	let n = t.map((t) => {
		let n = y(e.orientation, t.position)[2];
		return {
			id: t.id,
			path: we(e, t),
			depth: n,
			front: n > Ee(e, t) * Te
		};
	}).sort((e, t) => e.depth - t.depth);
	return {
		backPaths: n.filter((e) => !e.front).map((e) => e.path),
		frontPaths: n.filter((e) => e.front).map((e) => e.path),
		backNodeIds: n.filter((e) => !e.front).map((e) => e.id),
		frontNodeIds: n.filter((e) => e.front).map((e) => e.id)
	};
}, Oe = (e, t, n = 1, r = {}) => {
	let i = R(e, t, -1, n, r.eyeOffset), a = R(e, t, 1, n, r.eyeOffset), o = i.map((e) => e.point), s = a.map((e) => e.point), c = De(e, r.bodyNodes ?? []), l = xe(e, t);
	return {
		backPaths: [...l, ...c.backPaths],
		frontPaths: c.frontPaths,
		backNodeIds: [...l.map(() => null), ...c.backNodeIds],
		frontNodeIds: c.frontNodeIds,
		headPath: $(e, t),
		leftPath: D(o),
		rightPath: D(s),
		leftVisible: i.reduce((e, t) => e + t.normal[2], 0) > 0,
		rightVisible: a.reduce((e, t) => e + t.normal[2], 0) > 0,
		wirePaths: r.includeWire === !1 ? [] : B(e, t)
	};
};
//#endregion
export { i as RADIUS, S as axisVector, c as clamp, s as expressionFields, _ as expressionWithOrientation, ae as interpolatePose, d as multiplyQuaternions, u as normalizeQuaternion, O as poseFromExpression, f as quaternionFromAxisAngle, p as quaternionFromEuler, m as quaternionFromVectors, h as quaternionToEuler, l as radians, Oe as renderAvatar, ne as renderBodyNodeEditor, H as renderEyeEditor, E as rotateBodyNodeAroundLocalAxis, C as rotateExpressionAroundAxis, w as rotateExpressionAroundCamera, ee as rotateExpressionWithArcball, y as rotateWithQuaternion, te as rotationRing, v as slerpQuaternion, re as translateBodyNodeAlongLocalAxis, ie as translateBodyNodeInCameraPlane };

//# sourceMappingURL=geometry.js.map