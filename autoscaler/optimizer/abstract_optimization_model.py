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

from typing import Iterable


"""
Initialize the StateTracker with separate plots for each model and label aggregation.

Args:
    primary_label (str): The label that defines the start of a cycle (default: "Required").
    update_interval (float): Time between plot updates in seconds (default: 0.5).
    figsize (Tuple[int, int]): Figure size for each plot (default: (10, 6)).
    time_window_minutes (int): Time window in minutes for plotting history (default: 10).
    selected_models (Optional[List[str]]): List of up to 4 model IDs to plot; if None, uses first 4.
"""


class AbstractOptimizationModel:
    """
    This is a place holder class for an optimization solver class that specify which methods the class should contai
    """

    def add_continuous_vars(self, keys: Iterable, name: str) -> dict:
        """
        Creates continuous decision variables.
        :param keys: An iterable containing the indices for the variables.
        :type keys: Iterable
        :param name:  A string to be used as the base name for the variables.
        :type name: str
        :return: A dictionary mapping each key to its corresponding continuous variable.
        :rtype: dict
        """
        pass

    def add_binary_vars(self, keys: Iterable, name: str) -> dict:
        """
        Creates binary decision variables.
        :param keys: An iterable containing the indices for the variables.
        :type keys: Iterable
        :param name:  A string to be used as the base name for the variables.
        :type name: str
        :return: A dictionary mapping each key to its corresponding binary variable.
        :rtype: dict
        """

        pass

    def add_integer_vars(self, keys: Iterable, name: str) -> dict:
        """
        Creates integer decision variables.
        :param keys: An iterable containing the indices for the variables.
        :type keys: Iterable
        :param name:  A string to be used as the base name for the variables.
        :type name: str
        :return: A dictionary mapping each key to its corresponding integer variable.
        :rtype: dict
        """
        pass

    def add_constraint(self, ct, name: str):
        """
        Adds a single constraint to the model.
        :param ct: A constraint expression.
        :type: class expression
        :param name: A string identifier for the constraint.
        :type name: str

        """
        pass

    def add_constraints(self, cts, name: str):
        """
        Adds multiple constraints to the model.
        :param cts: constraint expressions.
        :type: class expressions/ Iterable of class expressions
        :param name: A base string identifier for the constraints.
        :type name: str
        """
        pass

    def add_objective(self, sense, expr):
        """
        Adds multiple constraints to the model.
        :param sense: The optimization direction (minimize/maximize).
        :type: str
        :param expr: The mathematical expression representing the objective function.
        :type: class expression
        """
        pass

    def add_expression(self):
        """
        Defines an auxiliary expression that can be used in constraints or objectives.
        Returns: An expression object.
        """
        pass

    def sum(self, args):
        """
        Computes the summation of a list of terms.
        :param args: The mathematical expression representing the objective function.
        :type: Iterable of class expressions
        :return: A collection of variables, coefficients, or expressions to be summed.
        """
        pass

    def solve(self):
        """
        Solves the optimization problem using an appropriate solver.
        :return: The solver's status or result.
        """
        pass

    def minimize(self, expr):
        """
        Sets the objective function to minimize objective.
        :param expr: A set of variables involved in the SOS1 constraint (only one can be nonzero in a feasible solution).
        """
        pass

    def maximize(self, expr):
        """
        Sets the objective function to maximize objective.
        :param expr: A set of variables involved in the SOS1 constraint (only one can be nonzero in a feasible solution).
        """
        pass

    def get_solution_values(self, var) -> dict | list | float | int:
        """
        Retrieves the values of optimal vars.
        :param var: A single variable or a collection of variables we want to retrieve.
        :return: The relevant solution values, either as a dictionary, list, float, or int.
        """
        pass

    def add_sos1(self, vars):
        """
        Adds a Special Ordered Set of Type 1 (SOS1) constraint.
        :param vars: A set of variables involved in the SOS1 constraint (only one can be nonzero in a feasible solution).
        """
        pass

    def clear(self):
        """
        Resets the optimization model, removing all variables, constraints, and objectives.
        """
        pass

    def print_lp(self):
        """
        Prints the optimization model
        """
        pass
