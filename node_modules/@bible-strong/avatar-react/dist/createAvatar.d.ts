import { type AnimationKey, type AvatarDefinition, type ExpressionKey } from '@bible-strong/avatar-core';
import type { ReactElement } from 'react';
import { type AvatarController, type AvatarProps } from './Avatar';
type AvatarDefinitionInput = {
    expressions: object;
    animations: object;
};
type StringKey<T> = Extract<keyof T, string>;
/** Props for a component created from one concrete avatar definition. */
export type CreatedAvatarProps<Definition extends AvatarDefinitionInput> = Omit<AvatarProps, 'definition' | 'animation' | 'expression' | 'defaultAnimation' | 'defaultExpression'> & {
    animation?: StringKey<Definition['animations']>;
    defaultAnimation?: StringKey<Definition['animations']>;
    expression?: StringKey<Definition['expressions']>;
    defaultExpression?: StringKey<Definition['expressions']>;
};
/** A concrete avatar component with animation and expression keys from its definition. */
export type CreatedAvatarComponent<Definition extends AvatarDefinitionInput> = (props: CreatedAvatarProps<Definition>) => ReactElement;
/**
 * Validate a JSON-compatible definition once and create a concrete React component from it.
 *
 * When the input is a statically typed definition, the returned component narrows its animation
 * and expression props to that definition's semantic keys. Values loaded at runtime are still
 * validated, but necessarily expose the broad string-key API at compile time.
 */
export declare function createAvatar<const Definition extends AvatarDefinitionInput>(definition: Definition): CreatedAvatarComponent<Definition>;
export declare function createAvatar(definition: unknown): CreatedAvatarComponent<AvatarDefinition>;
export type { AnimationKey, AvatarController, ExpressionKey };
//# sourceMappingURL=createAvatar.d.ts.map