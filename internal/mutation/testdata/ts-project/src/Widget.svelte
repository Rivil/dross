<!-- The Svelte construct fixture. <script>phantom()</script> here stays a comment. -->
<script module>
  export const step = 2;
</script>

<script lang="ts">
  let count: number = 0;

  function increment(by: number): number {
    count = count + by;
    return count;
  }

  function reset(): void {
    if (count > 0) {
      count = 0;
    }
  }
</script>

<button onclick={() => increment(step)}>
  {count + 1}
</button>

{#if count > 3}
  <p>many</p>
{:else}
  <button onclick={reset}>few</button>
{/if}

<style>
  p {
    color: red;
  }
</style>
