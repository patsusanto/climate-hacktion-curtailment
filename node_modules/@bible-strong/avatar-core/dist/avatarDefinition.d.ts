import type { SurfaceType } from './surfaces';
export declare const AVATAR_DEFINITION_MAX_BYTES = 262144;
export declare const AVATAR_DEFINITION_MAX_DEPTH = 32;
export declare const SEMANTIC_KEY_PATTERN: RegExp;
export type SemanticKeyIssueCode = 'missing_semantic_key' | 'invalid_semantic_key' | 'reserved_semantic_key';
export declare const getSemanticKeyIssue: (semanticKey: string | undefined, kind: "expression" | "animation") => SemanticKeyIssueCode | undefined;
export type SemanticKey = string;
export type ExpressionKey = SemanticKey;
export type AnimationKey = SemanticKey;
export type HexColor = `#${string}`;
export type AvatarColorsDefinition = {
    body: HexColor;
    eyes: HexColor;
};
export type SurfaceDefinition<TType extends SurfaceType = SurfaceType> = {
    type: TType;
    width: number;
    height: number;
    depth: number;
    roundness: number;
    morphRoundness?: number;
    tipRoundness?: number;
    baseRoundness?: number;
};
export type BodyNodeSurfaceType = Exclude<SurfaceType, 'mickey' | 'cursor'>;
export type PrimarySurfaceDefinition = SurfaceDefinition<SurfaceType>;
export type BodyNodeSurfaceDefinition = SurfaceDefinition<BodyNodeSurfaceType>;
export type AvatarBodyNodeDefinition = {
    surface: BodyNodeSurfaceDefinition;
    position: [number, number, number];
    rotation: [number, number, number];
};
export type AvatarBodyDefinition = {
    primary: PrimarySurfaceDefinition;
    nodes: AvatarBodyNodeDefinition[];
};
export type AvatarExpressionDefinition = {
    head: {
        x: number;
        y: number;
        z: number;
    };
    eyes: {
        left: {
            width: number;
            height: number;
            x: number;
            y: number;
            angle: number;
        };
        right: {
            width: number;
            height: number;
            x: number;
            y: number;
            angle: number;
        };
        spacing: number;
    };
    perspective: number;
    motion: {
        eyes: 'none' | 'microSaccades' | 'shake';
        body: 'none' | 'slowDrift' | 'shake';
    };
    colors?: Partial<AvatarColorsDefinition>;
};
export type AvatarAnimationStepDefinition = {
    expression: ExpressionKey;
    holdMs: number;
    transitionMs: number;
    transition: 'spring' | 'smooth' | 'snappy';
};
export type AvatarAnimationDefinition = {
    playbackMode: 'loop' | 'once' | 'pingPong';
    steps: AvatarAnimationStepDefinition[];
    blink: {
        enabled: boolean;
        initialDelayMs: number;
        minIntervalMs: number;
        maxIntervalMs: number;
        durationMs: number;
    };
    metadata?: {
        label?: string;
        description?: string;
        group?: string;
    };
};
export type AvatarDefinition = {
    schema: 'bible-strong/avatar-definition';
    schemaVersion: 1;
    name?: string;
    body: AvatarBodyDefinition;
    colors: AvatarColorsDefinition;
    expressions: Record<ExpressionKey, AvatarExpressionDefinition>;
    expressionOrder: ExpressionKey[];
    animations: Record<AnimationKey, AvatarAnimationDefinition>;
    animationOrder: AnimationKey[];
    /** @deprecated Accepted for pre-release JSON compatibility; it no longer adds animations. */
    standardAnimationSet?: 1;
};
export type AvatarDefinitionError = {
    path: string;
    code: string;
    message: string;
};
export type ValidationResult<T> = {
    ok: true;
    value: Readonly<T>;
} | {
    ok: false;
    errors: readonly AvatarDefinitionError[];
};
export declare const validateAvatarDefinition: (value: unknown) => ValidationResult<AvatarDefinition>;
export declare const parseAvatarDefinition: (text: string) => ValidationResult<AvatarDefinition>;
export declare const avatarDefinitionFileName: (name: string) => string;
//# sourceMappingURL=avatarDefinition.d.ts.map