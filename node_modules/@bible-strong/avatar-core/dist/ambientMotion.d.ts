import type { BodyMotion, Expression, EyeMotion } from './geometry';
export declare const eyeMotionModes: readonly ["none", "microSaccades", "shake"];
export declare const bodyMotionModes: readonly ["none", "slowDrift", "shake"];
export declare const isEyeMotion: (value: unknown) => value is EyeMotion;
export declare const isBodyMotion: (value: unknown) => value is BodyMotion;
export declare const hasAmbientMotion: (expression: Expression) => boolean;
export declare const ambientBodyOffset: (expression: Expression, elapsedMs: number, strength?: number) => {
    x: number;
    y: number;
};
export declare const ambientEyeOffset: (expression: Expression, elapsedMs: number, strength?: number) => {
    x: number;
    y: number;
};
export declare const applyAmbientBodyMotion: (expression: Expression, elapsedMs: number, strength?: number) => Expression;
export declare const applyAmbientMotion: (expression: Expression, elapsedMs: number, strength?: number) => Expression;
//# sourceMappingURL=ambientMotion.d.ts.map