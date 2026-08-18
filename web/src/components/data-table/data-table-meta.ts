import "@tanstack/react-table"

/**
 * 给列定义加一个 `meta.label`：列显隐菜单要显示人话，而 header 那一格
 * 常常是个带排序按钮的组件，取不出纯文本。
 *
 * 模块增强写在独立文件里，不夹在组件中间——它是全局类型声明，混在组件
 * 文件里容易被当成那个组件的私有约定。
 */
declare module "@tanstack/react-table" {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface ColumnMeta<TFeatures, TData, TValue> {
    /** 列显隐菜单里的显示名。 */
    label?: string
    /**
     * 同时贴到该列的表头与单元格。对齐与列宽必须两边一致，分开写迟早会
     * 有一边忘记改。
     */
    className?: string
  }
}
