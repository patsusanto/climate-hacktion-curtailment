//#region src/ambientMotion.ts
var e = [
	"none",
	"microSaccades",
	"shake"
], t = [
	"none",
	"slowDrift",
	"shake"
], n = new Set(e), r = new Set(t), i = (e) => typeof e == "string" && n.has(e), a = (e) => typeof e == "string" && r.has(e), o = (e) => e * e * (3 - 2 * e), s = (e) => {
	let t = Math.sin(e * 127.1 + 311.7) * 43758.5453;
	return (t - Math.floor(t)) * 2 - 1;
}, c = (e) => e.headX * .71 + e.headY * 1.13 + e.headZ * 1.37, l = 17.29, u = (e, t, n, r) => {
	let i = e / r, a = Math.floor(i), c = o(i - a), l = s(a * 3 + t + n);
	return l + (s((a + 1) * 3 + t + n) - l) * c;
}, d = (e, t, n) => {
	let r = 1100;
	if (e <= 0) return 0;
	let i = Math.floor(e / r), a = (e - i * r) / 140, c = o(Math.min(a, 1)), l = i === 0 ? 0 : s((i - 1) * 2 + t + n);
	return l + (s(i * 2 + t + n) - l) * c;
}, f = (e) => e.eyeMotion !== "none" || e.bodyMotion !== "none", p = (e, t, n = 1) => {
	let r = c(e);
	if (e.bodyMotion === "slowDrift") return {
		x: u(t, 3, r, 2900) * 1.45 * n,
		y: u(t, 4, r, 3700) * 1.1 * n
	};
	if (e.bodyMotion === "shake") {
		let e = t / 1e3;
		return {
			x: (Math.sin(e * 31) + Math.sin(e * 53) * .45) * 1.35 * n,
			y: (Math.sin(e * 37) + Math.sin(e * 61) * .4) * 1.1 * n
		};
	}
	return {
		x: 0,
		y: 0
	};
}, m = (e, t, n = 1) => {
	if (e.eyeMotion === "microSaccades") return {
		x: d(t, 0, l) * 1.5 * n,
		y: d(t, 1, l) * .9 * n
	};
	if (e.eyeMotion === "shake") {
		let e = t / 1e3;
		return {
			x: (Math.sin(e * 47) + Math.sin(e * 71) * .45) * 1.2 * n,
			y: (Math.sin(e * 59) + Math.sin(e * 83) * .4) * .8 * n
		};
	}
	return {
		x: 0,
		y: 0
	};
}, h = (e, t, n = 1) => {
	let r = { ...e }, i = c(e);
	if (e.bodyMotion === "slowDrift") r.headX += u(t, 0, i, 2600) * .8 * n, r.headY += u(t, 1, i, 3300) * 1.15 * n, r.headZ += u(t, 2, i, 4100) * .45 * n;
	else if (e.bodyMotion === "shake") {
		let e = t / 1e3;
		r.headX += (Math.sin(e * 31) + Math.sin(e * 53) * .45) * 1.15 * n, r.headY += (Math.sin(e * 37) + Math.sin(e * 61) * .4) * 1.35 * n, r.headZ += Math.sin(e * 43) * .7 * n;
	}
	return r;
}, g = (e, t, n = 1) => {
	let r = h(e, t, n), i = m(e, t, n);
	return r.positionXLeft += i.x, r.positionXRight += i.x, r.positionYLeft += i.y, r.positionYRight += i.y, r;
};
//#endregion
export { p as ambientBodyOffset, m as ambientEyeOffset, h as applyAmbientBodyMotion, g as applyAmbientMotion, t as bodyMotionModes, e as eyeMotionModes, f as hasAmbientMotion, a as isBodyMotion, i as isEyeMotion };

//# sourceMappingURL=ambientMotion.js.map