<script setup lang="ts">
import PolicyWorkspace from './PolicyWorkspace.vue'
defineProps<{api:(p:string,m?:string,b?:any)=>Promise<any>}>()
</script>
<template><PolicyWorkspace :api="api"/></template>
