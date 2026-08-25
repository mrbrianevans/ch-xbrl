"""Wide-row iXBRL parser used as a 38-column oracle.

Converted from stream-read-xbrl (`inspiration_stream_read_xbrl.py`). Mapping,
numeric formats, and priority rules are kept as in that parser. Zip/S3 bulk
ingest is dropped: this module only turns one instance into wide rows.
"""

from __future__ import annotations

import collections
import collections.abc
import datetime
import decimal
import io
import logging
import operator
import pathlib
import re
import typing
from dataclasses import dataclass
from itertools import chain

import dateutil.parser
import lxml.etree

if typing.TYPE_CHECKING:
    from lxml.etree import _Element as Element

# 38 columns (original stream-read-xbrl row, without the zip_url suffix).
COLUMNS = (
    "run_code",
    "company_id",
    "date",
    "file_type",
    "taxonomy",
    "balance_sheet_date",
    "companies_house_registered_number",
    "entity_current_legal_name",
    "company_dormant",
    "average_number_employees_during_period",
    "period_start",
    "period_end",
    "tangible_fixed_assets",
    "debtors",
    "cash_bank_in_hand",
    "current_assets",
    "creditors_due_within_one_year",
    "creditors_due_after_one_year",
    "net_current_assets_liabilities",
    "total_assets_less_current_liabilities",
    "net_assets_liabilities_including_pension_asset_liability",
    "called_up_share_capital",
    "profit_loss_account_reserve",
    "shareholder_funds",
    "turnover_gross_operating_revenue",
    "other_operating_income",
    "cost_sales",
    "gross_profit_loss",
    "administrative_expenses",
    "raw_materials_consumables",
    "staff_costs",
    "depreciation_other_amounts_written_off_tangible_intangible_fixed_assets",
    "other_operating_charges_format2",
    "operating_profit_loss",
    "profit_loss_on_ordinary_activities_before_tax",
    "tax_on_profit_or_loss_on_ordinary_activities",
    "profit_loss_for_period",
    "error",
)
_COLUMNS = COLUMNS

logger = logging.getLogger(__name__)

XBRLData = typing.Union[str, bool, decimal.Decimal, datetime.date, None]
XBRLRow = tuple[XBRLData, ...]

_PROD_NAME = re.compile(
    r"^(Prod\d+_\d+)_([^_]+)_(\d{8})\.(html|xml|zip)$",
    re.IGNORECASE,
)
_AA_NAME = re.compile(
    r"^(\d{8})_[^_]+_(\d{4}-\d{2}-\d{2})\.(xhtml|html|xml|htm)$",
    re.IGNORECASE,
)


def parse_source_name(name: str) -> tuple[str, str, datetime.date | None, str]:
    """Filename → (run_code, company_id, date, file_type)."""
    fn = pathlib.Path(name).name
    mo = _PROD_NAME.match(fn)
    if mo:
        run_code, company_id, ymd, filetype = mo.groups()
        return run_code, company_id, dateutil.parser.parse(ymd).date(), filetype.lower()
    mo = _AA_NAME.match(fn)
    if mo:
        company_id, iso, filetype = mo.groups()
        return "", company_id, datetime.date.fromisoformat(iso), filetype.lower()
    suffix = pathlib.Path(fn).suffix.lstrip(".").lower()
    stem = pathlib.Path(fn).stem
    m2 = re.match(r"^(\d{8})", stem)
    return "", m2.group(1) if m2 else "", None, suffix


def _xbrl_to_rows(
    name_xbrl_xml_str_orig: tuple[str, bytes],
) -> tuple[XBRLRow, ...]:
    name, xbrl_xml_str_orig = name_xbrl_xml_str_orig

    # Slightly hacky way to remove BOM, which is present in some older data
    xbrl_xml_str = io.BytesIO(xbrl_xml_str_orig[xbrl_xml_str_orig.find(b"<") :])

    # Low level value parsers

    def _date(text: str) -> datetime.date:
        return dateutil.parser.parse(text).date()

    def _parse(
        element: Element,
        text: str,
        parser: collections.abc.Callable[[Element, str], typing.Any],
    ) -> decimal.Decimal | None:
        null_value_markers = {
            "",
            "\u002d",  # Hyphen-Minus (ASCII)
            "\u2013",  # En dash
            "\u2014",  # Em dash
        }
        return parser(element, text.strip()) if text and text.strip() not in null_value_markers else None

    def _parse_str(_element: Element, text: str) -> str:
        return str(text).replace("\n", " ").replace('"', "")

    def _parse_absolute(element: Element, text: str) -> decimal.Decimal | None:
        # Some cases where employee numbers have a negative sign attached,
        # seemingly indicating negative employee numbers
        decimal = _parse_decimal_with_colon_or_dash(element, text)
        return abs(decimal) if decimal is not None else None

    def _parse_decimal(element: Element, text: str) -> decimal.Decimal:
        sign = -1 if element.get("sign", "") == "-" else +1
        text_without_thousands_separator_str = (
            text.replace(".", "").replace(",", ".")
            if element.get("format", "").rpartition(":")[2] == "numdotcomma"
            else text.replace(" ", "")
            if element.get("format", "").rpartition(":")[2] == "numspacedot"
            else text.replace(",", "")
        )
        if " " in text_without_thousands_separator_str:
            text_without_thousands_separator = sum(
                map(decimal.Decimal, text_without_thousands_separator_str.split(" "))
            )
        else:
            text_without_thousands_separator = decimal.Decimal(text_without_thousands_separator_str)
        return (
            sign
            * decimal.Decimal(text_without_thousands_separator)
            * decimal.Decimal(10) ** decimal.Decimal(element.get("scale", "0"))
        )

    def _parse_decimal_with_colon_or_dash(element: Element, text: str) -> decimal.Decimal | None:
        # Values seem to have a human readble prefix that isn't part of the value,
        # like "2017 - 2" to mean 2 employees. So we strip the prefix.
        return _parse(element, re.sub(r"(.*:)|(.+- )", "", text), _parse_decimal)

    def _parse_date(element: Element, text: str) -> datetime.date:
        date_format = element.get("format", "").rpartition(":")[2].lower()
        day_first = date_format in {"datedaymonthyear", "dateslasheu", "datedoteu"}
        if date_format == "datedaymonthyearen":
            text = text.replace(" ", "")
        text = re.sub(r"(?i)(\d)((st)|(nd)|(rd)|(th))", r"\1", text)
        try:
            return dateutil.parser.parse(text, dayfirst=day_first).date()
        except dateutil.parser.ParserError:
            # Try to parse mis-spellings that still have the first 3 characters right
            return dateutil.parser.parse(
                re.sub(r"([a-zA-Z]+)", lambda m: m.group(0)[:3], text), dayfirst=day_first
            ).date()

    def _parse_bool(_element: Element, text: str) -> bool | None:
        return False if text == "false" else True if text == "true" else None

    def _parse_reversed_bool(_element: Element, text: str) -> bool | None:
        return False if text == "true" else True if text == "false" else None

    # Parsing strategy
    #
    # The XBRL format is a "tagging" format that can tag elements in any order with machine readable metadata.
    # While flexible, this means that it's difficult to efficiently convert to a dataframe.
    #
    # The simplest way to do this would XPath repeatedly to find extract the data for each columnn. This was
    # done in previous versions, but took about 3 times as long as the current solution. The current solution
    # leverages the fact that dictionary lookups are fast, and so constructs dictionaries that can be looked up
    # while iterating through all the elements in the document.

    # Although in some cases a dictionary lookup doesn't seem possible, and so a custom matcher can be defined

    @dataclass
    class _TEST:
        name: str | None
        search: collections.abc.Callable[
            [Element, typing.Any, typing.Any, typing.Any],
            typing.Any,
        ] = lambda element, _local_name, _attribute_name, _context_ref: (element,)

    @dataclass
    class _TN(_TEST):
        # (Local) Tag name, i.e. withoout namespace
        pass

    @dataclass
    class _AV(_TEST):
        # Attribute value. Matches on the "name" attribute, but stripping off the namespace prefix
        pass

    @dataclass
    class _CUSTOM(_TEST):
        # Custom test when matching on tag name or name attribute isn't enought
        pass

    GENERAL_XPATH_MAPPINGS: dict[
        str,
        list[
            tuple[_TEST, collections.abc.Callable[[Element, str], str | bool | decimal.Decimal | datetime.date | None]]
        ],
    ] = {
        "balance_sheet_date": ([
            (_AV("BalanceSheetDate"), _parse_date),
            (_TN("BalanceSheetDate"), _parse_date),
        ]),
        "companies_house_registered_number": ([
            (_AV("UKCompaniesHouseRegisteredNumber"), _parse_str),
            (_TN("CompaniesHouseRegisteredNumber"), _parse_str),
        ]),
        "entity_current_legal_name": ([
            (
                _AV(
                    "EntityCurrentLegalOrRegisteredName",
                    lambda element, _local_name, _attribute_name, _context_ref: chain(
                        (element,),
                        typing.cast("list[Element]", element.xpath("./*[local-name()='span'][1]")),
                    ),
                ),
                _parse_str,
            ),
            (
                _TN(
                    "EntityCurrentLegalName",
                    lambda element, _local_name, _attribute_name, _context_ref: chain(
                        (element,),
                        typing.cast("list[Element]", element.xpath("./*[local-name()='span'][1]")),
                    ),
                ),
                _parse_str,
            ),
        ]),
        "company_dormant": ([
            (_AV("EntityDormantTruefalse"), _parse_bool),
            (_AV("EntityDormant"), _parse_bool),
            (_TN("CompanyDormant"), _parse_bool),
            (_TN("CompanyNotDormant"), _parse_reversed_bool),
        ]),
        "average_number_employees_during_period": ([
            (_AV("AverageNumberEmployeesDuringPeriod"), _parse_absolute),
            (_AV("EmployeesTotal"), _parse_absolute),
            (_TN("AverageNumberEmployeesDuringPeriod"), _parse_absolute),
            (_TN("EmployeesTotal"), _parse_absolute),
        ]),
    }

    PERIODICAL_XPATH_MAPPINGS: dict[
        str,
        list[
            tuple[_TEST, collections.abc.Callable[[Element, str], str | bool | decimal.Decimal | datetime.date | None]]
        ],
    ] = {
        # balance sheet
        "tangible_fixed_assets": ([
            (_TN("FixedAssets"), _parse_decimal),
            (_AV("FixedAssets"), _parse_decimal),
            (_TN("TangibleFixedAssets"), _parse_decimal),
            (_AV("TangibleFixedAssets"), _parse_decimal),
            (_AV("PropertyPlantEquipment"), _parse_decimal),
        ]),
        "debtors": ([
            (_TN("Debtors"), _parse_decimal),
            (_AV("Debtors"), _parse_decimal),
        ]),
        "cash_bank_in_hand": ([
            (_TN("CashBankInHand"), _parse_decimal),
            (_AV("CashBankInHand"), _parse_decimal),
            (_AV("CashBankOnHand"), _parse_decimal),
        ]),
        "current_assets": ([
            (_TN("CurrentAssets"), _parse_decimal),
            (_AV("CurrentAssets"), _parse_decimal),
        ]),
        "creditors_due_within_one_year": ([
            (_AV("CreditorsDueWithinOneYear"), _parse_decimal),
            (
                _AV(
                    "Creditors",
                    lambda element, _local_name, _attribute_name, _context_ref: (
                        (element,) if "WithinOneYear" in element.get("contextRef", "") else ()
                    ),
                ),
                _parse_decimal,
            ),
        ]),
        "creditors_due_after_one_year": ([
            (_AV("CreditorsDueAfterOneYear"), _parse_decimal),
            (
                _CUSTOM(
                    None,
                    lambda element, local_name, _attribute_name, context_ref: (
                        (element,) if local_name == "Creditors" and "AfterOneYear" in context_ref else ()
                    ),
                ),
                _parse_decimal,
            ),
        ]),
        "net_current_assets_liabilities": ([
            (_TN("NetCurrentAssetsLiabilities"), _parse_decimal),
            (_AV("NetCurrentAssetsLiabilities"), _parse_decimal),
        ]),
        "total_assets_less_current_liabilities": ([
            (_TN("TotalAssetsLessCurrentLiabilities"), _parse_decimal),
            (_AV("TotalAssetsLessCurrentLiabilities"), _parse_decimal),
        ]),
        "net_assets_liabilities_including_pension_asset_liability": ([
            (_TN("NetAssetsLiabilitiesIncludingPensionAssetLiability"), _parse_decimal),
            (_AV("NetAssetsLiabilitiesIncludingPensionAssetLiability"), _parse_decimal),
            (_TN("NetAssetsLiabilities"), _parse_decimal),
            (_AV("NetAssetsLiabilities"), _parse_decimal),
        ]),
        "called_up_share_capital": ([
            (_TN("CalledUpShareCapital"), _parse_decimal),
            (_AV("CalledUpShareCapital"), _parse_decimal),
            (
                _CUSTOM(
                    None,
                    lambda element, _local_name, attribute_name, _context_ref: (
                        (element,)
                        if attribute_name == "Equity" and "ShareCapital" in element.get("contextRef", "")
                        else ()
                    ),
                ),
                _parse_decimal,
            ),
        ]),
        "profit_loss_account_reserve": ([
            (_TN("ProfitLossAccountReserve"), _parse_decimal),
            (_AV("ProfitLossAccountReserve"), _parse_decimal),
            (
                _CUSTOM(
                    None,
                    lambda element, _local_name, attribute_name, _context_ref: (
                        (element,)
                        if attribute_name == "Equity"
                        and "RetainedEarningsAccumulatedLosses" in element.get("contextRef", "")
                        else ()
                    ),
                ),
                _parse_decimal,
            ),
        ]),
        "shareholder_funds": ([
            (_TN("ShareholderFunds"), _parse_decimal),
            (_AV("ShareholderFunds"), _parse_decimal),
            (
                _CUSTOM(
                    None,
                    lambda element, _local_name, attribute_name, context_ref: (
                        (element,) if attribute_name == "Equity" and "segment" not in context_ref else ()
                    ),
                ),
                _parse_decimal,
            ),
        ]),
        # income statement
        "turnover_gross_operating_revenue": ([
            (_TN("TurnoverGrossOperatingRevenue"), _parse_decimal),
            (_AV("TurnoverGrossOperatingRevenue"), _parse_decimal),
            (_TN("TurnoverRevenue"), _parse_decimal),
            (_AV("TurnoverRevenue"), _parse_decimal),
        ]),
        "other_operating_income": ([
            (_TN("OtherOperatingIncome"), _parse_decimal),
            (_AV("OtherOperatingIncome"), _parse_decimal),
            (_TN("OtherOperatingIncomeFormat2"), _parse_decimal),
            (_AV("OtherOperatingIncomeFormat2"), _parse_decimal),
        ]),
        "cost_sales": ([
            (_TN("CostSales"), _parse_decimal),
            (_AV("CostSales"), _parse_decimal),
        ]),
        "gross_profit_loss": ([
            (_TN("GrossProfitLoss"), _parse_decimal),
            (_AV("GrossProfitLoss"), _parse_decimal),
        ]),
        "administrative_expenses": ([
            (_TN("AdministrativeExpenses"), _parse_decimal),
            (_AV("AdministrativeExpenses"), _parse_decimal),
        ]),
        "raw_materials_consumables": ([
            (_TN("RawMaterialsConsumables"), _parse_decimal),
            (_AV("RawMaterialsConsumables"), _parse_decimal),
            (_TN("RawMaterialsConsumablesUsed"), _parse_decimal),
            (_AV("RawMaterialsConsumablesUsed"), _parse_decimal),
        ]),
        "staff_costs": ([
            (_TN("StaffCosts"), _parse_decimal),
            (_AV("StaffCosts"), _parse_decimal),
            (_TN("StaffCostsEmployeeBenefitsExpense"), _parse_decimal),
            (_AV("StaffCostsEmployeeBenefitsExpense"), _parse_decimal),
        ]),
        "depreciation_other_amounts_written_off_tangible_intangible_fixed_assets": ([
            (_TN("DepreciationOtherAmountsWrittenOffTangibleIntangibleFixedAssets"), _parse_decimal),
            (_AV("DepreciationOtherAmountsWrittenOffTangibleIntangibleFixedAssets"), _parse_decimal),
            (_TN("DepreciationAmortisationImpairmentExpense"), _parse_decimal),
            (_AV("DepreciationAmortisationImpairmentExpense"), _parse_decimal),
        ]),
        "other_operating_charges_format2": ([
            (_TN("OtherOperatingChargesFormat2"), _parse_decimal),
            (_AV("OtherOperatingChargesFormat2"), _parse_decimal),
            (_TN("OtherOperatingExpensesFormat2"), _parse_decimal),
            (_AV("OtherOperatingExpensesFormat2"), _parse_decimal),
        ]),
        "operating_profit_loss": ([
            (_TN("OperatingProfitLoss"), _parse_decimal),
            (_AV("OperatingProfitLoss"), _parse_decimal),
        ]),
        "profit_loss_on_ordinary_activities_before_tax": ([
            (_TN("ProfitLossOnOrdinaryActivitiesBeforeTax"), _parse_decimal),
            (_AV("ProfitLossOnOrdinaryActivitiesBeforeTax"), _parse_decimal),
        ]),
        "tax_on_profit_or_loss_on_ordinary_activities": ([
            (_TN("TaxOnProfitOrLossOnOrdinaryActivities"), _parse_decimal),
            (_AV("TaxOnProfitOrLossOnOrdinaryActivities"), _parse_decimal),
            (_TN("TaxTaxCreditOnProfitOrLossOnOrdinaryActivities"), _parse_decimal),
            (_AV("TaxTaxCreditOnProfitOrLossOnOrdinaryActivities"), _parse_decimal),
        ]),
        "profit_loss_for_period": ([
            (_TN("ProfitLoss"), _parse_decimal),
            (_AV("ProfitLoss"), _parse_decimal),
            (_TN("ProfitLossForPeriod"), _parse_decimal),
            (_AV("ProfitLossForPeriod"), _parse_decimal),
        ]),
    }

    all_mappings = dict(**GENERAL_XPATH_MAPPINGS, **PERIODICAL_XPATH_MAPPINGS)

    tag_name_test_dict = {
        test.name: (name, priority, test, parser)
        for (name, tests) in all_mappings.items()
        for (priority, (test, parser)) in enumerate(tests)
        if isinstance(test, _TN)
    }

    attribute_value_test_dict = {
        test.name: (name, priority, test, parser)
        for (name, tests) in all_mappings.items()
        for (priority, (test, parser)) in enumerate(tests)
        if isinstance(test, _AV)
    }

    custom_tests = tuple(
        (name, priority, test, parser)
        for (name, tests) in all_mappings.items()
        for (priority, (test, parser)) in enumerate(tests)
        if isinstance(test, _CUSTOM)
    )

    def _get_dates(context: Element) -> tuple[str | bytes | None, str | bytes | None]:
        instant_elements = typing.cast("Element", context.xpath("./*[local-name()='instant']"))
        start_date_text_nodes = typing.cast("str", context.xpath("./*[local-name()='startDate']/text()"))
        end_date_text_nodes = typing.cast("str", context.xpath("./*[local-name()='endDate']/text()"))
        return (
            (None, None)
            if context is None
            else (instant_elements[0].text.strip(), instant_elements[0].text.strip())
            if instant_elements and instant_elements[0].text
            else (None, None)
            if start_date_text_nodes[0] is None or end_date_text_nodes[0] is None
            else (start_date_text_nodes[0].strip(), end_date_text_nodes[0].strip())
        )

    try:
        document = lxml.etree.parse(xbrl_xml_str, lxml.etree.XMLParser(ns_clean=True, recover=True))
        root = document.getroot()
        document.xpath("//*[0]")
    except (lxml.etree.Error, AssertionError):
        # In at least one case - Prod224_9956_04944372_20100331.xml, the XML seems very badly formed.
        # Suspect this is before Companies House had better validation. The best we can do is log and
        # carry on. We can at least still get a row in the data
        logger.warning("Bad XML. Name: %s XML: %s", name, xbrl_xml_str_orig)
        document = lxml.etree.parse(
            io.BytesIO(b'<?xml version="1.0" encoding="UTF-8"?><root></root>'),
            lxml.etree.XMLParser(ns_clean=True, recover=True),
        )
        root = document.getroot()

    context_dates = {
        e.get("id"): _get_dates(period)
        for e in typing.cast("list[Element]", document.xpath("//*[local-name()='context']"))
        for period in typing.cast("list[Element]", e.xpath("./*[local-name()='period']"))[:1]
    }

    fn = pathlib.Path(name).name
    # CH bulk: Prod223_4203_00134794_20250927.html
    # Sample / AA: 03024914_aa_2023-03-13.xhtml
    # Some April 2021 data files end in .zip, but seem to really be html.
    run_code, company_id, file_date, filetype = parse_source_name(fn)
    if file_date is None and not company_id:
        logger.warning("Unrecognised filename; parsing XBRL anyway: %s", fn)
    allowed_taxonomies = [
        "http://www.xbrl.org/uk/fr/gaap/pt/2004-12-01",
        "http://www.xbrl.org/uk/gaap/core/2009-09-01",
        "http://xbrl.frc.org.uk/fr/2014-09-01/core",
    ]

    core_attributes = (
        run_code,
        company_id,
        file_date,
        filetype,
        ";".join(set(allowed_taxonomies) & set(root.nsmap.values())),
    )

    # Mutable dictionaries to store the "priority" (lower is better) of a found value
    general_attributes_with_priorities: dict[str, tuple[int, decimal.Decimal | None]] = dict.fromkeys(
        GENERAL_XPATH_MAPPINGS.keys(), (10, None)
    )
    periodic_attributes_with_priorities: collections.defaultdict[
        typing.Any, dict[str, tuple[int, decimal.Decimal | None]]
    ] = collections.defaultdict(lambda: dict.fromkeys(PERIODICAL_XPATH_MAPPINGS.keys(), (10, None)))

    def tag_name_tests(
        local_name: str,
    ) -> typing.Generator[tuple[str, int, _TN, collections.abc.Callable[[Element, str], typing.Any]]]:
        tag_name_test = tag_name_test_dict.get(local_name)
        if tag_name_test is not None:
            yield from (tag_name_test,)

    def attribute_value_tests(
        attribute_value: str,
    ) -> typing.Generator[tuple[str, int, _AV, collections.abc.Callable[[Element, str], typing.Any]]]:
        attribute_value_test = attribute_value_test_dict.get(attribute_value)
        if attribute_value_test is not None:
            yield from (attribute_value_test,)

    def handle_general(
        element: Element,
        local_name: str,
        attribute_value: str,
        context_ref: str,
        name: str,
        priority: int,
        test: _TEST,
        parse: collections.abc.Callable[[Element, str], typing.Any],
    ) -> None:
        best_priority, _best_value = general_attributes_with_priorities[name]

        if priority > best_priority:
            return

        for found_element in test.search(element, local_name, attribute_value, context_ref):
            filtered = ((e.text or "") for e in found_element.iter() if e.tag.rpartition("}")[2] != "exclude")
            value = _parse(found_element, "".join(filtered), parse)
            if value is not None:
                general_attributes_with_priorities[name] = (priority, value)
                break

    def handle_periodic(
        element: Element,
        local_name: str,
        attribute_value: str,
        context_ref: str,
        name: str,
        priority: int,
        test: _TEST,
        parse: collections.abc.Callable[[Element, str], typing.Any],
    ) -> None:
        if not context_ref:
            return
        dates = context_dates.get(context_ref)
        if not dates:
            return

        for found_element in test.search(element, local_name, attribute_value, context_ref):
            best_priority, _best_value = periodic_attributes_with_priorities[dates][name]

            if priority >= best_priority:
                return

            filtered = ((e.text or "") for e in found_element.iter() if lxml.etree.QName(e).localname != "exclude")
            value = _parse(found_element, "".join(filtered), parse)
            if value is not None:
                periodic_attributes_with_priorities[dates][name] = (priority, value)
                break

    error = None
    try:
        for element in typing.cast("list[Element]", document.xpath("//*")):
            _, _, local_name = element.tag.rpartition("}")
            _, _, attribute_value = element.get("name", "").rpartition(":")
            context_ref = element.get("contextRef", "")

            for name, priority, test, parse in chain(
                tag_name_tests(local_name), attribute_value_tests(attribute_value), custom_tests
            ):
                handler = handle_general if name in general_attributes_with_priorities else handle_periodic

                handler(element, local_name, attribute_value, context_ref, name, priority, test, parse)

        general_attributes = tuple(general_attributes_with_priorities[name][1] for name in GENERAL_XPATH_MAPPINGS)

        periods = tuple(
            (
                datetime.date.fromisoformat(period_start_end[0]),
                datetime.date.fromisoformat(period_start_end[1]),
                *tuple(periodic_attributes[name][1] for name in PERIODICAL_XPATH_MAPPINGS),
            )
            for period_start_end, periodic_attributes in periodic_attributes_with_priorities.items()
        )
        sorted_periods = sorted(periods, key=operator.itemgetter(0, 1), reverse=True)
    except ValueError as e:
        error = str(e)
        return (
            (core_attributes + (None,) * (2 + len(GENERAL_XPATH_MAPPINGS) + len(PERIODICAL_XPATH_MAPPINGS)) + (error,)),
        )

    return (
        tuple((core_attributes + general_attributes + period + (None,)) for period in sorted_periods)
        if sorted_periods
        else ((core_attributes + general_attributes + (None,) * (3 + len(PERIODICAL_XPATH_MAPPINGS))),)
    )


def _cell(v: XBRLData) -> str:
    if v is None:
        return ""
    if isinstance(v, datetime.date):
        return v.isoformat()
    if isinstance(v, bool):
        return "true" if v else "false"
    if isinstance(v, decimal.Decimal):
        return format(v, "f")
    return str(v)


def rows_as_dicts(rows: tuple[XBRLRow, ...]) -> list[dict[str, str]]:
    out: list[dict[str, str]] = []
    for row in rows:
        d = {col: _cell(row[i] if i < len(row) else None) for i, col in enumerate(COLUMNS)}
        out.append(d)
    return out


def parse_instance(path: pathlib.Path | str) -> list[dict[str, str]]:
    """Parse one iXBRL file into 38-column wide rows (string cells)."""
    path = pathlib.Path(path)
    data = path.read_bytes()
    rows = _xbrl_to_rows((path.name, data))
    return rows_as_dicts(rows)


