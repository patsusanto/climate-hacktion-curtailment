# @bible-strong/avatar-react

React 19 renderer for a validated Bible Strong `AvatarDefinition`. React and React DOM 19 are peer
dependencies; `@bible-strong/avatar-core` is installed as a normal dependency.

## Install

```sh
pnpm add @bible-strong/avatar-react react react-dom
```

Import the package stylesheet once in the application entry point:

```tsx
import { Avatar, type AvatarController } from '@bible-strong/avatar-react'
import type { AvatarDefinition } from '@bible-strong/avatar-core'
import '@bible-strong/avatar-react/styles.css'
import { useRef } from 'react'

export function Assistant({ definition }: { definition: AvatarDefinition }) {
  const avatar = useRef<AvatarController>(null)
  return (
    <>
      <Avatar ref={avatar} definition={definition} defaultAnimation="idle" />
      <button onClick={() => avatar.current?.play('happy')}>Play happy</button>
      <button onClick={() => avatar.current?.setExpression('neutral')}>Neutral</button>
    </>
  )
}
```

For a reusable component tied to one JSON definition, use `createAvatar`. It validates the
definition once and, when the JSON is statically typed, narrows `animation`, `defaultAnimation`,
`expression` and `defaultExpression` to the semantic keys present in that definition:

```tsx
import { createAvatar } from '@bible-strong/avatar-react'
import avatarJson from './strobi.avatar.json'

const StrobiAvatar = createAvatar(avatarJson)

export function Strobi() {
  return <StrobiAvatar defaultAnimation="idle" />
}
```

Definitions fetched at runtime are validated by the same factory, but their keys are necessarily
checked at runtime rather than inferred by TypeScript.

`play` and `setExpression` return `{ ok: true }` or a typed error with one of
`unknown_animation`, `unknown_expression` or `controlled_by_props`. `pause` freezes the exact
timeline position, calling `play` with the paused key resumes it, and `stop` returns an
uncontrolled avatar to `neutral`.

## Props reference

`Avatar` exposes typed props for the definition, playback state and presentation. `AnimationKey`
and `ExpressionKey` are semantic string keys from the supplied definition.

### Definition and playback

| Prop                | Type                                 | Default  | Behavior                                                                                                                   |
| ------------------- | ------------------------------------ | -------- | -------------------------------------------------------------------------------------------------------------------------- |
| `definition`        | `AvatarDefinition`                   | required | Validated JSON definition containing the expressions and animations to render.                                             |
| `animation`         | `AnimationKey \| undefined`          | —        | Controlled timeline. Each step chooses the displayed expression. Mutually exclusive with `expression`.                     |
| `expression`        | `ExpressionKey \| undefined`         | —        | Controlled direct expression. Mutually exclusive with `animation`.                                                         |
| `defaultAnimation`  | `AnimationKey \| undefined`          | —        | Initial uncontrolled timeline, read on mount. Autoplay is enabled by default. Mutually exclusive with `defaultExpression`. |
| `defaultExpression` | `ExpressionKey \| undefined`         | —        | Initial uncontrolled expression, read on mount without starting a timeline. Mutually exclusive with `defaultAnimation`.    |
| `autoplay`          | `boolean \| undefined`               | `true`   | Starts `defaultAnimation` automatically. It has no effect without `defaultAnimation`.                                      |
| `ref`               | `Ref<AvatarController> \| undefined` | —        | Exposes the imperative API described below.                                                                                |

`animation` and `expression` are two alternative sources of truth. Passing both throws an error;
the component never silently overrides one with the other. A controlled target takes priority over
an uncontrolled default when they are intentionally mixed.

### Presentation

| Prop        | Type                            | Default             | Behavior                                                                    |
| ----------- | ------------------------------- | ------------------- | --------------------------------------------------------------------------- |
| `size`      | `number \| string \| undefined` | `240`               | Number or CSS value applied to the wrapper width and height.                |
| `className` | `string \| undefined`           | —                   | CSS class added to the outer wrapper.                                       |
| `style`     | `CSSProperties \| undefined`    | —                   | Inline styles for the outer wrapper. `width` and `height` come from `size`. |
| `ariaLabel` | `string \| undefined`           | `Procedural avatar` | Accessible name announced to screen readers.                                |

### Playback callbacks

| Prop                 | Type                                  | Receives                                                               |
| -------------------- | ------------------------------------- | ---------------------------------------------------------------------- |
| `onAnimationEnd`     | `(animation: AnimationKey) => void`   | The key of a `once` animation when it completes naturally.             |
| `onExpressionChange` | `(expression: ExpressionKey) => void` | The semantic expression key whenever the displayed expression changes. |
| `onError`            | `(error: AvatarRuntimeError) => void` | An unknown animation or expression key supplied through props.         |

Unknown keys passed through `animation`, `expression`, `defaultAnimation` or `defaultExpression`
are reported to `onError`. Without an error handler, the component writes the typed runtime error
to the developer console instead of failing silently.

## Imperative API

Pass a `ref` to receive an `AvatarController`. Use it when buttons, events or another imperative
source need to drive an uncontrolled avatar:

| Method                      | Type                                                 | Behavior                                                       |
| --------------------------- | ---------------------------------------------------- | -------------------------------------------------------------- |
| `play(animation)`           | `(animation: AnimationKey) => AvatarCommandResult`   | Starts an animation or resumes it from its paused position.    |
| `pause()`                   | `() => void`                                         | Freezes the exact timeline position.                           |
| `stop()`                    | `() => void`                                         | Stops playback and resets an uncontrolled avatar to `neutral`. |
| `setExpression(expression)` | `(expression: ExpressionKey) => AvatarCommandResult` | Shows one expression directly.                                 |
| `getState()`                | `() => AvatarPlaybackState`                          | Returns `activeAnimation?`, `activeExpression` and `status`.   |

`play` and `setExpression` return `{ ok: true }` or `{ ok: false, error }`. Errors include
`unknown_animation`, `unknown_expression` and `controlled_by_props`. When `animation` or
`expression` is controlled by props, use those props to change the target; imperative target
commands cannot replace the parent value.

The definition is validated once per immutable object reference and revalidated/reinitialized when
that reference changes.

## Styling hooks

The stylesheet exposes `.bs-avatar` and `.bs-avatar__svg`. Consumer `className` and `style` are
applied to the outer wrapper.

The public component never exposes Studio IDs or document types. The package follows Semantic
Versioning. While it remains below `1.0.0`, breaking API changes increment the minor version and
fixes increment the patch version.
