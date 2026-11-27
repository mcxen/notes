import { describe, expect, it } from "vitest";
import { inlineFromText } from "./inline-content";
import {
  measureTableColumnDemands,
  newSimpleTable,
  resolveTableColumnWidths,
  setColumnWidths,
  TABLE_COLUMN_WIDTH,
  tableFromCells,
  tableGrid,
} from "./simple-table";

let seq = 0;
const id = (kind: "column" | "row") => `${kind === "column" ? "col" : "row"}_${(seq += 1)}`;
const sum = (values: readonly number[]) => values.reduce((total, value) => total + value, 0);

describe("measureTableColumnDemands", () => {
  it("returns 0 for empty or whitespace-only columns and measures ASCII, CJK, and multiline cells", () => {
    seq = 0;
    const grid = tableGrid(
      tableFromCells(
        [
          [inlineFromText(""), inlineFromText("   \n  "), inlineFromText("Key"), inlineFromText("中文参数说明"), inlineFromText("short\nlonger line here")],
          [inlineFromText(""), inlineFromText("\t"), inlineFromText("id"), inlineFromText("短"), inlineFromText("ok")],
        ],
        id,
      ),
    );
    const demands = measureTableColumnDemands(grid);
    const [c1, c2, c3, c4, c5] = grid.columns.map((column) => column.id);

    expect(demands[c1]).toBe(0);
    expect(demands[c2]).toBe(0);
    // "Key": 3 * 8 + 24 = 48
    expect(demands[c3]).toBe(48);
    // "中文参数说明": 6 * 14 + 24 = 108
    expect(demands[c4]).toBe(108);
    // "longer line here": 14 letters * 8 + 2 spaces * 4.5 + 24 = 145
    expect(demands[c5]).toBe(145);
  });
});

describe("resolveTableColumnWidths", () => {
  it("expands pasted 2- and 3-column tables to fill 696px and 1100px containers proportionally", () => {
    seq = 0;
    const twoCol = tableGrid(
      tableFromCells(
        [
          [inlineFromText("参数"), inlineFromText("这是一段比较长的中文说明文字，用于描述该配置项的具体作用与使用注意事项")],
          [inlineFromText("mode"), inlineFromText("auto")],
        ],
        id,
      ),
    );
    const twoDemands = measureTableColumnDemands(twoCol);
    const widths696 = resolveTableColumnWidths(twoCol.columns, 696, twoDemands);
    expect(sum(widths696)).toBe(696);
    expect(widths696[1]).toBeGreaterThan(widths696[0]);
    expect(widths696[0]).toBeGreaterThanOrEqual(72);

    const widths1100 = resolveTableColumnWidths(twoCol.columns, 1100, twoDemands);
    expect(sum(widths1100)).toBe(1100);
    expect(widths1100[1]).toBeGreaterThan(widths1100[0]);

    const threeCol = tableGrid(
      tableFromCells(
        [
          [inlineFromText("ID"), inlineFromText("Status"), inlineFromText("Detailed explanation about why this table should fill the reading column")],
        ],
        id,
      ),
    );
    const threeDemands = measureTableColumnDemands(threeCol);
    const three696 = resolveTableColumnWidths(threeCol.columns, 696, threeDemands);
    expect(sum(three696)).toBe(696);
    expect(three696[2]).toBeGreaterThan(three696[1]);
    expect(three696[1]).toBeGreaterThanOrEqual(three696[0]);
  });

  it("fills 696px equally for an empty 3-column table", () => {
    seq = 0;
    const grid = tableGrid(newSimpleTable(3, 2, id));
    const demands = measureTableColumnDemands(grid);
    expect(resolveTableColumnWidths(grid.columns, 696, demands)).toEqual([232, 232, 232]);
  });

  it("fills 696px for a 1-column table and caps at 1200px on a 1600px container", () => {
    seq = 0;
    const grid = tableGrid(tableFromCells([[inlineFromText("Only column")]], id));
    const demands = measureTableColumnDemands(grid);
    expect(resolveTableColumnWidths(grid.columns, 696, demands)).toEqual([696]);
    expect(resolveTableColumnWidths(grid.columns, 1600, demands)).toEqual([TABLE_COLUMN_WIDTH.max]);
  });

  it("caps a long column at 1200px and gives remaining width to its sibling on wide containers", () => {
    seq = 0;
    const grid = tableGrid(
      tableFromCells(
        [[inlineFromText("Key"), inlineFromText("a".repeat(200))]],
        id,
      ),
    );
    const demands = measureTableColumnDemands(grid);
    expect(resolveTableColumnWidths(grid.columns, 1600, demands)).toEqual([400, 1200]);
  });

  it("does not crush columns in wide multi-column tables and exceeds container for horizontal scroll", () => {
    seq = 0;
    const tenEmpty = tableGrid(newSimpleTable(10, 2, id));
    const widthsEmpty = resolveTableColumnWidths(tenEmpty.columns, 696, measureTableColumnDemands(tenEmpty));
    expect(widthsEmpty).toEqual(Array(10).fill(TABLE_COLUMN_WIDTH.fallback));
    expect(sum(widthsEmpty)).toBe(1200);

    const eightCols = tableGrid(
      tableFromCells(
        [Array.from({ length: 8 }, (_, index) => inlineFromText(`Column ${index + 1} content`))],
        id,
      ),
    );
    const widthsEight = resolveTableColumnWidths(eightCols.columns, 696, measureTableColumnDemands(eightCols));
    expect(widthsEight.every((width) => width >= 72 && width <= 260)).toBe(true);
    expect(sum(widthsEight)).toBeGreaterThan(696);
  });

  it("handles narrow 320px containers by fitting 2 columns and scrolling 5 columns", () => {
    seq = 0;
    const twoCols = tableGrid(tableFromCells([[inlineFromText("Name"), inlineFromText("Value")]], id));
    const two320 = resolveTableColumnWidths(twoCols.columns, 320, measureTableColumnDemands(twoCols));
    expect(sum(two320)).toBe(320);
    expect(two320.every((width) => width >= 72)).toBe(true);

    const fiveCols = tableGrid(newSimpleTable(5, 2, id));
    const five320 = resolveTableColumnWidths(fiveCols.columns, 320, measureTableColumnDemands(fiveCols));
    expect(sum(five320)).toBeGreaterThan(320);
    expect(five320.every((width) => width >= 72)).toBe(true);
  });

  it("preserves explicit column widths in manual mode and gives unset columns a fallback", () => {
    const columns = [
      { id: "col_1", width: 150 },
      { id: "col_2", width: 220 },
      { id: "col_3" },
    ];
    expect(resolveTableColumnWidths(columns, 696, { col_1: 500, col_2: 500, col_3: 0 })).toEqual([150, 220, 120]);
    expect(resolveTableColumnWidths(columns, 696, { col_1: 500, col_2: 500, col_3: 200 })).toEqual([150, 220, 200]);
  });

  it("returns sensible fallback widths when containerWidth is 0", () => {
    seq = 0;
    const grid = tableGrid(
      tableFromCells(
        [[inlineFromText(""), inlineFromText("Short"), inlineFromText("A".repeat(60))]],
        id,
      ),
    );
    const widths = resolveTableColumnWidths(grid.columns, 0, measureTableColumnDemands(grid));
    expect(widths).toEqual([120, 120, 260]);
  });
});

describe("setColumnWidths", () => {
  it("updates multiple columns atomically and normalizes widths", () => {
    seq = 0;
    const grid = tableGrid(newSimpleTable(3, 1, id));
    const [c1, c2, c3] = grid.columns.map((column) => column.id);
    const write = setColumnWidths(grid, { [c1]: 90.4, [c2]: 2000, [c3]: 12 });
    expect(write).toEqual({
      kind: "columns",
      columns: [
        { id: c1, width: 90 },
        { id: c2, width: TABLE_COLUMN_WIDTH.max },
        { id: c3, width: TABLE_COLUMN_WIDTH.min },
      ],
    });
    expect(setColumnWidths(grid, { missing: 100 })).toBeNull();
  });
});
