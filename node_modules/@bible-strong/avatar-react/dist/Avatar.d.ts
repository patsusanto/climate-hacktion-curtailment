import { type AnimationKey, type AvatarDefinition, type AvatarPlaybackState as CorePlaybackState, type AvatarRuntimeError as CoreRuntimeError, type ExpressionKey } from '@bible-strong/avatar-core';
import { type CSSProperties, type ReactElement, type Ref } from 'react';
import './styles.css';
export declare const markAvatarDefinitionValidated: (definition: object) => void;
export type AvatarRuntimeError = CoreRuntimeError | {
    code: 'controlled_by_props';
    key: string;
    message: string;
};
export type AvatarCommandResult = {
    ok: true;
} | {
    ok: false;
    error: AvatarRuntimeError;
};
export type AvatarPlaybackState = Pick<CorePlaybackState, 'activeAnimation' | 'activeExpression' | 'status'>;
export type AvatarController = {
    play(animation: AnimationKey): AvatarCommandResult;
    setExpression(expression: ExpressionKey): AvatarCommandResult;
    pause(): void;
    stop(): void;
    getState(): AvatarPlaybackState;
};
export type AvatarProps = {
    definition: AvatarDefinition;
    ref?: Ref<AvatarController>;
    /** Controlled animation timeline. Mutually exclusive with `expression`. */
    animation?: AnimationKey;
    /** Controlled expression target. Mutually exclusive with `animation`. */
    expression?: ExpressionKey;
    /** Uncontrolled initial animation. Mutually exclusive with `defaultExpression`. */
    defaultAnimation?: AnimationKey;
    /** Uncontrolled initial expression. Mutually exclusive with `defaultAnimation`. */
    defaultExpression?: ExpressionKey;
    autoplay?: boolean;
    size?: number | string;
    className?: string;
    style?: CSSProperties;
    ariaLabel?: string;
    /** Receives invalid animation or expression targets supplied through props. */
    onError?: (error: AvatarRuntimeError) => void;
    onAnimationEnd?: (animation: AnimationKey) => void;
    onExpressionChange?: (expression: ExpressionKey) => void;
};
export declare function Avatar({ definition, ref, animation, expression, defaultAnimation, defaultExpression, autoplay, size, className, style, ariaLabel, onError, onAnimationEnd, onExpressionChange, }: AvatarProps): ReactElement;
//# sourceMappingURL=Avatar.d.ts.map