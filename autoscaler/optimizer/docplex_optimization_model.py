"""
------------------------------------------------------ {COPYRIGHT-TOP} ---
IBM Confidential
OCO Source Materials
IBM Watson Machine Learning Core

Copyright IBM Corp. 2024. All Rights Reserved.

The source code for this program is not published or otherwise
divested of its trade secrets, irrespective of what has been
deposited with the U.S. Copyright Office.
------------------------------------------------------ {COPYRIGHT-END} ---
"""

from typing import Callable, Iterable

from docplex.mp.dvar import Var
from docplex.mp.model import Model

from optimizer.abstract_optimization_model import AbstractOptimizationModel


class DoCplexOptimizationModel(AbstractOptimizationModel):
    """
    This class implements an optimization model using IBM CPLEX optimization tool.
    """

    def __init__(self, name: str):
        """
        Initializes the CPLEX model with a given name.
        :param name: Name of the optimization model.
        :type name: str
        """
        self.mdl = Model(name)

    def add_continuous_vars(self, keys: Iterable, name: str, lb: float | Iterable | Callable = None,
                            ub: float | Iterable | Callable = None) -> dict:
        """
        Creates continuous decision variables.
        :param keys: An iterable containing indices for the variables.
        :type keys: Iterable
        :param name: Base name for the variables.
        :type name: str
        :param lb: Lower bound(s) for the variables (single value, iterable, or callable).
        :type lb: float | Iterable | Callable, optional
        :param ub: Upper bound(s) for the variables (single value, iterable, or callable).
        :type ub: float | Iterable | Callable, optional
        :return: A dictionary mapping each key to its corresponding continuous variable.
        :rtype: dict
        """

        return self.mdl.continuous_var_dict(keys, lb, ub, name)

    def add_binary_vars(self, keys: Iterable, name: str) -> dict:
        """
        Creates binary decision variables.
        :param keys: An iterable containing indices for the variables.
        :type keys: Iterable
        :param name: Base name for the variables.
        :type name: str
        :return: A dictionary mapping each key to its corresponding binary variable.
        :rtype: dict
        """
        return self.mdl.binary_var_dict(keys, name=name)

    def add_integer_vars(self, keys: Iterable, name: str, lb: float | Iterable | Callable = None,
                            ub: float | Iterable | Callable = None) -> dict:
        """
        Creates integer decision variables.
        :param keys: An iterable containing indices for the variables.
        :type keys: Iterable
        :param name: Base name for the variables.
        :type name: str
        :param lb: Lower bound(s) for the variables (single value, iterable, or callable).
        :type lb: float | Iterable | Callable, optional
        :param ub: Upper bound(s) for the variables (single value, iterable, or callable).
        :type ub: float | Iterable | Callable, optional
        :return: A dictionary mapping each key to its corresponding integer variable.
        :rtype: dict
        """
        return self.mdl.integer_var_dict(keys, lb, ub, name)

    def add_constraint(self, ct, name: str):
        """
        Adds a single constraint to the model.
        :param ct: A constraint expression.
        :type ct: Expression
        :param name: Identifier for the constraint.
        :type name: str
        """
        return self.mdl.add_constraint(ct, ctname=name)

    def add_constraints(self, cts, name: str):
        """
        Adds multiple constraints to the model.
        :param cts: A collection of constraint expressions.
        :type cts: Iterable[Expression]
        :param name: Base identifier for the constraints.
        :type name: str
        """
        self.mdl.add_constraints(cts, names=name)

    def add_objective(self, sense, expr):
        """
        Sets the objective function for the model.
        :param sense: Optimization direction (minimize/maximize).
        :type sense: str
        :param expr: Expression representing the objective function.
        :type expr: Expression
        """
        return self.mdl.set_objective(sense, expr)

    def add_expression(self):
        """
        Defines an auxiliary expression that can be used in constraints or objectives.
        """
        super().add_expression()

    def sum(self, args):
        """
        Computes the summation of a list of terms.
        :param args: Collection of terms to sum.
        :type args: Iterable[Expression]
        :return: Summation expression.
        :rtype: Expression
        """
        return self.mdl.sum(args)

    def solve(self):
        """
        Solves the optimization problem using the CPLEX solver.
        :return: The solution object containing optimal values.
        :rtype: Solution
        """
        self.mdl.solve()
        return self.mdl.solution

    def minimize(self, expr):
        """
        Sets the objective function to minimize the given expression.
        :param expr: Expression to minimize.
        :type expr: Expression
        """
        return self.mdl.minimize(expr)

    def maximize(self, expr):
        """
        Sets the objective function to maximize the given expression.
        :param expr: Expression to maximize.
        :type expr: Expression
        """
        return self.mdl.maximize(expr)

    def get_solution_values(self, var: dict[..., Var] | list[Var] | Var) -> dict | list | float | int:
        """
        Retrieves the values of the decision variables.
        :param var: A single variable, a list, or a dictionary of variables.
        :return: Solution values as a dictionary, list, float, or int.
        """

        if isinstance(var, dict):
            return {k: v.solution_value for k, v in var.items()}
        if isinstance(var, list):
            return [v.solution_value for v in var]
        if isinstance(var, Var):
            return var.solution_value
        else:
            raise Exception("Unsupported decision variable")

    def add_sos1(self, vars):
        """
        Adds a Special Ordered Set of Type 1 (SOS1) constraint.
        :param vars: Set of variables involved in the SOS1 constraint.
        """
        return self.mdl.add_sos1(vars)

    def print_lp(self):
        """
        Presents the optimization model.
        :return: The LP string and variable names.
        :rtype: tuple[str, list[str]]
        """

        lp_string = self.mdl.export_as_lp_string()
        variable_names = [var.name for var in self.mdl.iter_variables()]
        return lp_string, variable_names

    def abs(self, expr):
        """
        Computes the absolute value of an expression.
        :param expr: The expression to compute the absolute value for.
        :type expr: Expression
        """
        return self.mdl.abs(expr)

    def clear(self):
        """
        Clears the model, removing all variables, constraints, and objectives.
        """
        self.mdl.clear()
