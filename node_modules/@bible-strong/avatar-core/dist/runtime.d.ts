import { type AnimationKey, type AvatarAnimationDefinition, type AvatarDefinition, type AvatarExpressionDefinition, type ExpressionKey } from './avatarDefinition';
import { type Expression } from './geometry';
import { type AvatarScene } from './scene';
export type AvatarRuntimeError = {
    code: 'unknown_animation' | 'unknown_expression';
    key: string;
    message: string;
};
export type AvatarCommandResult<T> = {
    ok: true;
    value: T;
} | {
    ok: false;
    error: AvatarRuntimeError;
};
export declare const resolveExpression: (definition: Readonly<AvatarDefinition>, key: ExpressionKey) => AvatarCommandResult<Readonly<AvatarExpressionDefinition>>;
export declare const resolveAnimation: (definition: Readonly<AvatarDefinition>, key: AnimationKey) => AvatarCommandResult<Readonly<AvatarAnimationDefinition>>;
export type AvatarPlaybackState = {
    activeAnimation?: AnimationKey;
    activeExpression: ExpressionKey;
    status: 'playing' | 'paused' | 'stopped';
    stepIndex: number;
    direction: 1 | -1;
    phase: 'transition' | 'hold';
    phaseStartedAt: number;
    transitionFrom: ExpressionKey;
    transitionSnapshot?: AvatarFrameSnapshot;
    pausedAt?: number;
    blinkDueAt?: number;
    blinkStartedAt?: number;
    directTransition?: {
        from: AvatarFrameSnapshot;
        startedAt: number;
        durationMs: number;
        transition: AvatarAnimationDefinition['steps'][number]['transition'];
    };
};
export type AvatarFrameSnapshot = {
    expression: Expression;
    colors: AvatarScene['colors'];
};
export type AvatarRuntimeEnvironment = {
    random: () => number;
    reduceMotion?: boolean;
};
export declare const createAvatarPlaybackState: () => AvatarPlaybackState;
export declare const playAvatarAnimation: (definition: Readonly<AvatarDefinition>, key: AnimationKey, now: number, from?: AvatarFrameSnapshot) => AvatarCommandResult<AvatarPlaybackState>;
export declare const advanceAvatarPlayback: (definition: Readonly<AvatarDefinition>, state: Readonly<AvatarPlaybackState>, now: number, environment: AvatarRuntimeEnvironment) => AvatarPlaybackState;
export declare const pauseAvatarPlayback: (state: Readonly<AvatarPlaybackState>, now: number) => AvatarPlaybackState;
export declare const resumeAvatarPlayback: (state: Readonly<AvatarPlaybackState>, now: number) => AvatarPlaybackState;
export declare const blinkOpacityAt: (animation: Readonly<AvatarAnimationDefinition>, state: Readonly<AvatarPlaybackState>, now: number) => number;
export declare const sampleAvatarFrame: (definition: Readonly<AvatarDefinition>, state: Readonly<AvatarPlaybackState>, now: number, environment: AvatarRuntimeEnvironment) => AvatarFrameSnapshot & {
    blink: number;
    sampledAt: number;
};
export declare const renderAvatarFrame: (definition: Readonly<AvatarDefinition>, state: Readonly<AvatarPlaybackState>, now: number, environment: AvatarRuntimeEnvironment) => AvatarScene;
//# sourceMappingURL=runtime.d.ts.map