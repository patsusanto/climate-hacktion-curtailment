import type { AvatarBodyDefinition, AvatarDefinition, AvatarExpressionDefinition, ExpressionKey } from './avatarDefinition';
import type { AvatarBody } from './body';
import { type AvatarGeometry, type Expression } from './geometry';
export declare const expressionFromDefinition: (key: ExpressionKey, expression: AvatarExpressionDefinition) => Expression;
export declare const bodyFromDefinition: (body: AvatarBodyDefinition) => AvatarBody;
export type AvatarScene = {
    geometry: AvatarGeometry;
    colors: {
        body: string;
        eyes: string;
    };
};
export declare const renderAvatarExpression: (definition: Readonly<AvatarDefinition>, expression: Expression, colors?: {
    body?: string;
    eyes?: string;
}, blink?: number) => AvatarScene;
export declare const renderAvatarDefinition: (definition: Readonly<AvatarDefinition>, expressionKey?: ExpressionKey) => AvatarScene;
//# sourceMappingURL=scene.d.ts.map