import numpy as np
import pandas as pd
from scipy.optimize import curve_fit

# -----------------------------------------
# Helper function: Load Data
# -----------------------------------------
def load_data(csv_path):
    df = pd.read_csv(csv_path)
    df["ell_i"] = df["ell_i"].astype(float)
    df["prefill_latency"] = df["prefill_latency"].astype(float)
    return df

# -----------------------------------------
# Estimate alpha (attention compute term) and C_0 (base overhead)
# -----------------------------------------
def estimate_alpha(df):
    df["ell_sq"] = df["ell_i"] ** 2
    R = df["R"].mean()
    X = df["ell_sq"] / R
    Y = df["prefill_latency"]
    def linear(x, alpha, c0):
        return alpha * x + c0
    popt, _ = curve_fit(linear, X, Y)
    return popt[0], popt[1]

# -----------------------------------------
# FlashAttention Efficiency Curve
# -----------------------------------------
def flash_attention_efficiency(df):
    def phi_fn(ell, beta):
        return 1 / (1 + beta * np.log(ell))
    df["phi_empirical"] = df["T_FA"] / df["T_noFA"]
    popt, _ = curve_fit(phi_fn, df["ell_i"], df["phi_empirical"])
    beta = popt[0]
    return beta, lambda ell: 1 / (1 + beta * np.log(ell))

# -----------------------------------------
# Estimate gamma: base paging cost
# -----------------------------------------
def estimate_gamma(df, block_size=16):
    df["num_pages"] = np.ceil(df["ell_i"] / block_size)
    X = df["num_pages"]
    Y = df["paging_latency"]
    def linear(x, gamma):
        return gamma * x
    gamma, _ = curve_fit(linear, X, Y)
    return gamma[0]

# -----------------------------------------
# Estimate transfer penalties delta and eta
# -----------------------------------------
def estimate_transfer_penalty(df, ell_thresh):
    df["is_large"] = df["ell_i"] > ell_thresh
    df["excess"] = (df["ell_i"] - ell_thresh).clip(lower=0)
    def transfer_model(x, delta, eta):
        ell, excess = x
        return delta * ell + eta * excess
    X = (df["ell_i"], df["excess"])
    Y = df["transfer_latency"]
    popt, _ = curve_fit(transfer_model, X, Y)
    return popt[0], popt[1]

# -----------------------------------------
# Estimate cache hit effects
# -----------------------------------------
def cache_effects(df):
    df_hit = df[df["cache_hit"] == 1]
    df_miss = df[df["cache_hit"] == 0]
    compute_reduction = 1 - df_hit["compute_latency"].mean() / df_miss["compute_latency"].mean()
    paging_reduction = 1 - df_hit["paging_latency"].mean() / df_miss["paging_latency"].mean()
    transfer_reduction = 1 - df_hit["transfer_latency"].mean() / df_miss["transfer_latency"].mean()
    return {
        "gamma_h": compute_reduction,
        "phi_h": paging_reduction,
        "tau_h": transfer_reduction,
    }

# -----------------------------------------
# Estimate kappa terms in superlinear paging
# -----------------------------------------
def estimate_rho(df, block_size):
    df["L"] = df["ell_i"]
    df["rolling_avg"] = df["ell_i"].rolling(window=5).mean()
    df["rolling_std"] = df["ell_i"].rolling(window=5).std()
    df["ratio"] = df["rolling_std"] / df["rolling_avg"]
    df["L_over_P"] = df["L"] / block_size
    df["mu"] = df["gpu_cache_util"]
    def rho_model(X, k1, k2, k3):
        L_over_P, ratio, mu = X
        return k1 * L_over_P * ratio + k2 * mu**2 + k3
    X = (df["L_over_P"].fillna(0), df["ratio"].fillna(0), df["mu"].fillna(0))
    Y = df["superlinear_paging_delay"].fillna(0)
    popt, _ = curve_fit(rho_model, X, Y)
    return popt[0], popt[1], popt[2]

# -----------------------------------------
# Estimate C_0, C_1 from latency vs. batch size
# -----------------------------------------
def estimate_c0_c1(df):
    def linear(x, c1, c0):
        return c1 * x + c0
    X = df["R"]
    Y = df["prefill_latency"]
    popt, _ = curve_fit(linear, X, Y)
    return popt[1], popt[0]

# -----------------------------------------
# Estimate continuous batching delay
# -----------------------------------------
def estimate_batching_delay(df):
    df["gap"] = df["request_start_time"].diff().fillna(0)
    return df["gap"].mean()
