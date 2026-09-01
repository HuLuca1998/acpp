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
    /**
     * 横向滚动时把这一列钉在左沿或右沿。
     *
     * 定的规矩是「关键信息左固定，操作按钮右固定」：表一宽，横向滚动就
     * 会把这两样一起卷走——中间的字段读到一半不知道是哪一行的，右边的
     * 编辑/删除更是要先滚回去才点得到。
     *
     * 同一侧可以钉多列，但**必须是连续的首列或末列**：偏移是按相邻的
     * 固定列宽度累加出来的，中间夹一列会滚的就对不上了。
     */
    pin?: "left" | "right"
  }
}
